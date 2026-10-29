package modem

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
)

// scriptedTransport answers every command with a canned response and records
// what was written, so the AT dialog is tested without hardware.
type scriptedTransport struct {
	mu      sync.Mutex
	pending []byte
	writes  []string
	reply   func(command string) string
	// chunk, when set, delivers a reply in slices of at most this many bytes, the
	// way a serial link hands bytes over instead of the whole answer at once.
	chunk  int
	closed bool
}

func (t *scriptedTransport) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	command := string(p)
	t.writes = append(t.writes, command)
	t.pending = append(t.pending, []byte(t.reply(command))...)
	return len(p), nil
}

func (t *scriptedTransport) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.pending) == 0 {
		return 0, os.ErrDeadlineExceeded
	}
	n := copy(p, t.pending)
	if t.chunk > 0 && n > t.chunk {
		n = t.chunk
	}
	t.pending = t.pending[n:]
	return n, nil
}

func (t *scriptedTransport) SetReadDeadline(time.Time) error { return nil }

func (t *scriptedTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

func (t *scriptedTransport) written() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.writes...)
}

// slowBodyTransport acknowledges the message body only after delay, and answers
// every other command at once: the shape of a modem waiting for the network
// before it reports +CMGS (ADR 0022).
type slowBodyTransport struct {
	mu       sync.Mutex
	pending  []byte
	replyAt  time.Time
	deadline time.Time
	delay    time.Duration
}

func (t *slowBodyTransport) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	command := string(p)
	if strings.ContainsRune(command, '\x1a') {
		t.replyAt = time.Now().Add(t.delay)
		t.pending = append(t.pending, []byte("\r\n+CMGS: 9\r\nOK\r\n")...)
		return len(p), nil
	}
	t.pending = append(t.pending, []byte(okReply(command))...)
	return len(p), nil
}

func (t *slowBodyTransport) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if time.Now().Before(t.replyAt) {
		// A real port blocks here until the deadline instead of failing early.
		if !t.deadline.IsZero() && !time.Now().Before(t.deadline) {
			return 0, os.ErrDeadlineExceeded
		}
		time.Sleep(time.Millisecond)
		return 0, nil
	}
	if len(t.pending) == 0 {
		return 0, os.ErrDeadlineExceeded
	}
	n := copy(p, t.pending)
	t.pending = t.pending[n:]
	return n, nil
}

func (t *slowBodyTransport) SetReadDeadline(deadline time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.deadline = deadline
	return nil
}

func (t *slowBodyTransport) Close() error { return nil }

// okReply is a modem that accepts everything.
func okReply(command string) string {
	switch {
	case strings.HasPrefix(command, "AT+CMGS="):
		return "\r\n> "
	case strings.Contains(command, "\x1a"):
		return "\r\n+CMGS: 7\r\nOK\r\n"
	default:
		return "\r\nOK\r\n"
	}
}

func newTestModem(transport Transport) *Modem {
	return NewWithTransport(
		Settings{Device: "fake", Baud: 115200, CommandTimeout: time.Second},
		transport,
		zap.NewNop(),
	)
}

func TestSendRunsTheFullATDialog(t *testing.T) {
	transport := &scriptedTransport{reply: okReply}

	if err := newTestModem(transport).Send(context.Background(), "628123456789", "PIN: 123456"); err != nil {
		t.Fatalf("send: %v", err)
	}

	written := strings.Join(transport.written(), "")
	for _, want := range []string{"AT+CMGS=\"628123456789\"", "PIN: 123456\x1a"} {
		if !strings.Contains(written, want) {
			t.Fatalf("modem never received %q; wrote %q", want, written)
		}
	}
	if strings.Count(written, "AT+CMGS=") != 1 {
		t.Fatalf("dialog sent %d messages, want exactly 1: %q",
			strings.Count(written, "AT+CMGS="), written)
	}
}

func TestSendReturnsAnErrorWhenTheModemRejects(t *testing.T) {
	transport := &scriptedTransport{reply: func(command string) string {
		if strings.HasPrefix(command, "AT+CMGS=") {
			return "\r\nERROR\r\n"
		}
		return "\r\nOK\r\n"
	}}

	err := newTestModem(transport).Send(context.Background(), "628123456789", "PIN: 123456")
	if err == nil {
		t.Fatal("expected an error when the modem answers ERROR")
	}
	if !strings.Contains(err.Error(), "ERROR") {
		t.Fatalf("error %q does not mention the modem answer", err)
	}
	// The AT vocabulary is what an operator needs: "modem answered ERROR" alone
	// left the real production failure unattributable for several attempts.
	if !strings.Contains(err.Error(), "AT+CMGS") {
		t.Fatalf("error %q does not name the rejected command", err)
	}
}

// The dialog asks the modem for numeric error reports, because a refusal that
// carries the modem's own code is the difference between "top up the SIM" and
// "check the dialog" (ADR 0023).
func TestSendEnablesExtendedErrorReports(t *testing.T) {
	transport := &scriptedTransport{reply: okReply}

	// A session is opened lazily, so this is where the setup commands belong;
	// NewWithTransport (used by the dialog tests) skips them on purpose.
	modem := newWithOpener(
		Settings{Device: "fake", Baud: 115200, CommandTimeout: time.Second},
		func(Settings) (Transport, error) { return transport, nil },
		zap.NewNop(),
	)
	if err := modem.Send(context.Background(), "628123456789", "PIN: 123456"); err != nil {
		t.Fatalf("send: %v", err)
	}

	written := strings.Join(transport.written(), "")
	enabled := strings.Index(written, "AT+CMEE=1")
	if enabled < 0 {
		t.Fatalf("the session never enabled extended error reports: %q", written)
	}
	if firstCommand := strings.Index(written, "AT+CMGF=1"); enabled > firstCommand {
		t.Fatalf("extended errors were enabled after the first command that can fail: %q", written)
	}
}

// A firmware that does not know AT+CMEE must still deliver: the rejection is only
// debug-logged and the dialog goes on.
func TestSendContinuesWhenTheModemRejectsExtendedReports(t *testing.T) {
	transport := &scriptedTransport{reply: func(command string) string {
		if command == "AT+CMEE=1\r" {
			return "\r\nERROR\r\n"
		}
		return okReply(command)
	}}

	modem := newWithOpener(
		Settings{Device: "fake", Baud: 115200, CommandTimeout: time.Second},
		func(Settings) (Transport, error) { return transport, nil },
		zap.NewNop(),
	)
	if err := modem.Send(context.Background(), "628123456789", "PIN: 123456"); err != nil {
		t.Fatalf("a modem without CMEE support could not send: %v", err)
	}
}

// A refusal that carries the modem's own code says what happened; the bare ERROR
// on 27 Okt 11:39 left an operator to guess (ADR 0023).
func TestSendReportsTheModemsOwnErrorCode(t *testing.T) {
	transport := &scriptedTransport{reply: func(command string) string {
		if strings.ContainsRune(command, '\x1a') {
			return "\r\n+CMS ERROR: 500\r\n"
		}
		return okReply(command)
	}}

	err := newTestModem(transport).Send(context.Background(), "628123456789", "PIN: 123456")
	if err == nil {
		t.Fatal("expected the refused body to fail the send")
	}
	if !strings.Contains(err.Error(), "AT+CMGS body: modem answered +CMS ERROR: 500") {
		t.Fatalf("error %q does not carry the modem's own cause", err)
	}
}

// A serial link can deliver the refusal token and its numeric cause in two reads.
// Consuming the line at the token would report "+CMS ERROR" with no cause and
// leave ": 500" to answer the next command (ADR 0023).
func TestSendReportsTheCauseWhenTheRefusalArrivesInTwoReads(t *testing.T) {
	transport := &scriptedTransport{
		// 13 bytes ends the first slice exactly at the token: "\r\n+CMS ERROR:".
		chunk: 13,
		reply: func(command string) string {
			if strings.ContainsRune(command, '\x1a') {
				return "\r\n+CMS ERROR: 500\r\n"
			}
			return okReply(command)
		},
	}

	err := newTestModem(transport).Send(context.Background(), "628123456789", "PIN: 123456")
	if err == nil {
		t.Fatal("expected the refused body to fail the send")
	}
	if !strings.Contains(err.Error(), "AT+CMGS body: modem answered +CMS ERROR: 500") {
		t.Fatalf("error %q lost the modem's own cause", err)
	}
}

// The dialog's duration is part of the diagnosis: a refusal after twenty seconds
// of network wait is a different fault from one that came back at once (ADR 0023).
func TestSendLogsHowLongAFailedDialogTook(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	transport := &scriptedTransport{reply: func(command string) string {
		if strings.ContainsRune(command, '\x1a') {
			return "\r\n+CMS ERROR: 500\r\n"
		}
		return okReply(command)
	}}
	modem := NewWithTransport(
		Settings{Device: "fake", Baud: 115200, CommandTimeout: time.Second},
		transport,
		zap.New(core),
	)

	if err := modem.Send(context.Background(), "628123456789", "PIN: 123456"); err == nil {
		t.Fatal("expected the refused body to fail the send")
	}

	entries := logs.FilterMessage("modem dialog failed").All()
	if len(entries) != 1 {
		t.Fatalf("logged %d dialog failures, want 1", len(entries))
	}
	if elapsed, ok := entries[0].ContextMap()["elapsed"]; !ok || elapsed == nil {
		t.Fatalf("the failure line carries no elapsed time: %v", entries[0].ContextMap())
	}
}

// A body the GSM alphabet cannot carry, or one longer than one text-mode
// segment, is answered with a bare ERROR by the modem. It is refused before the
// device is touched, and the reason says so (ADR 0010).
func TestSendRefusesABodyThatCannotBeSent(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "more than one segment", body: strings.Repeat("a", 161), want: sms.ErrTooLong},
		{name: "outside the GSM alphabet", body: "PIN: 123456 \U0001F680", want: sms.ErrUnencodable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &scriptedTransport{reply: okReply}

			err := newTestModem(transport).Send(context.Background(), "628123456789", test.body)
			if !errors.Is(err, test.want) {
				t.Fatalf("send = %v, want %v", err, test.want)
			}
			if !sms.IsUnsendable(err) {
				t.Fatalf("send = %v, want an unsendable body error", err)
			}
			if written := transport.written(); len(written) != 0 {
				t.Fatalf("the modem was touched for an undeliverable body: %q", written)
			}
		})
	}
}

// A rejected body leaves the modem waiting at the ">" prompt, and that prompt
// survives the port being closed: ESC must cancel it before Close, otherwise the
// next session's commands are swallowed as message text.
func TestSendCancelsAPendingPromptBeforeClosing(t *testing.T) {
	transport := &scriptedTransport{reply: func(command string) string {
		if strings.HasPrefix(command, "AT+CMGS=") {
			return "\r\n> "
		}
		if strings.ContainsRune(command, '\x1a') {
			return "\r\nERROR\r\n"
		}
		return "\r\nOK\r\n"
	}}

	if err := newTestModem(transport).Send(context.Background(), "628123456789", "PIN: 123456"); err == nil {
		t.Fatal("expected the rejected body to fail the send")
	}

	written := strings.Join(transport.written(), "")
	if !strings.HasSuffix(written, "\x1b") {
		t.Fatalf("the dialog ended on %q, want the ESC that cancels the prompt", written)
	}
	if !transport.closed {
		t.Fatal("a failed dialog must still close the transport")
	}
}

// A crash in a previous session can leave an answer (or a prompt) in the port.
// Opening the device clears it, so the first real command is answered by the
// command itself.
func TestOpeningClearsAStaleAnswer(t *testing.T) {
	transport := &scriptedTransport{reply: okReply}
	// The leftover a closed session leaves behind: read before anything is written.
	transport.pending = []byte("\r\nERROR\r\n")

	modem := newWithOpener(
		Settings{Device: "fake", Baud: 115200, CommandTimeout: time.Second},
		func(Settings) (Transport, error) { return transport, nil },
		zap.NewNop(),
	)

	if err := modem.Send(context.Background(), "628123456789", "PIN: 123456"); err != nil {
		t.Fatalf("a stale answer from the previous session poisoned this one: %v", err)
	}
	if !strings.Contains(strings.Join(transport.written(), ""), "AT+CMGS=") {
		t.Fatal("the modem was never asked to send the message")
	}
}

func TestSendReopensAfterAFailedDialog(t *testing.T) {
	first := &scriptedTransport{reply: func(string) string { return "\r\nERROR\r\n" }}
	second := &scriptedTransport{reply: okReply}

	var mu sync.Mutex
	opened := 0
	transports := []Transport{first, second}

	modem := newWithOpener(
		Settings{Device: "fake", Baud: 115200, CommandTimeout: time.Second},
		func(Settings) (Transport, error) {
			mu.Lock()
			defer mu.Unlock()
			transport := transports[opened]
			opened++
			return transport, nil
		},
		zap.NewNop(),
	)

	if err := modem.Send(context.Background(), "628123456789", "PIN: 123456"); err == nil {
		t.Fatal("first send should fail")
	}
	if !first.closed {
		t.Fatal("a failed dialog must close the transport so the next attempt is clean")
	}
	if err := modem.Send(context.Background(), "628123456789", "PIN: 123456"); err != nil {
		t.Fatalf("second send: %v", err)
	}
	if opened != 2 {
		t.Fatalf("modem opened %d transports, want 2", opened)
	}
}

func TestSendFailsWhenTheDeviceCannotBeOpened(t *testing.T) {
	modem := newWithOpener(
		Settings{Device: "/dev/does-not-exist", Baud: 115200, CommandTimeout: time.Second},
		func(Settings) (Transport, error) { return nil, errors.New("no such device") },
		zap.NewNop(),
	)

	err := modem.Send(context.Background(), "628123456789", "PIN: 123456")
	if err == nil || !strings.Contains(err.Error(), "no such device") {
		t.Fatalf("expected the open error, got %v", err)
	}
}

func TestSendHonoursAnAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	transport := &scriptedTransport{reply: okReply}
	if err := newTestModem(transport).Send(ctx, "628123456789", "PIN: 123456"); err == nil {
		t.Fatal("expected a canceled context to stop the send")
	}
	if len(transport.written()) != 0 {
		t.Fatal("a canceled send must not talk to the modem")
	}
}

// A modem hands the body to the SMSC before it answers +CMGS, and that hand-off
// can outlast an AT round trip. Paying it from the command budget marked a
// delivery failed while the modem was still working, and the retry put a second
// copy of the link on the wire (ADR 0022).
func TestSendWaitsForTheBodySubmissionBeyondTheCommandBudget(t *testing.T) {
	transport := &slowBodyTransport{delay: 60 * time.Millisecond}
	modem := NewWithTransport(Settings{
		Device:         "fake",
		Baud:           115200,
		CommandTimeout: 20 * time.Millisecond,
		SubmitTimeout:  time.Second,
	}, transport, zap.NewNop())

	if err := modem.Send(context.Background(), "628123456789", "PIN: 123456"); err != nil {
		t.Fatalf("a body acknowledged after the command budget failed the send: %v", err)
	}
}

// The submission budget is what a silent body is measured against, and the error
// names it, so the log line says which wait expired.
func TestSendReportsTheSubmissionBudgetWhenTheBodyStaysSilent(t *testing.T) {
	transport := &slowBodyTransport{delay: time.Hour}
	modem := NewWithTransport(Settings{
		Device:         "fake",
		Baud:           115200,
		CommandTimeout: 20 * time.Millisecond,
		SubmitTimeout:  40 * time.Millisecond,
	}, transport, zap.NewNop())

	err := modem.Send(context.Background(), "628123456789", "PIN: 123456")
	if err == nil {
		t.Fatal("a modem that never answers the body must fail the send")
	}
	if !strings.Contains(err.Error(), "AT+CMGS body") || !strings.Contains(err.Error(), "40ms") {
		t.Fatalf("error %q does not name the step and its budget", err)
	}
}

func TestMatchTokenReportsTheEarliestAnswer(t *testing.T) {
	token, ok := matchToken("\r\nERROR\r\nOK\r\n", []string{"OK", "ERROR", "+CMS ERROR"})
	if !ok || token != "ERROR" {
		t.Fatalf("matchToken = (%q, %v), want ERROR", token, ok)
	}

	// With numeric reports on, "+CME ERROR" must win over the "ERROR" inside it,
	// or the failure loses the very prefix that names its kind (ADR 0023).
	token, ok = matchToken("\r\n+CME ERROR: 3\r\n", []string{"OK", "ERROR", "+CME ERROR"})
	if !ok || token != "+CME ERROR" {
		t.Fatalf("matchToken = (%q, %v), want +CME ERROR", token, ok)
	}

	if _, ok := matchToken("nothing yet", []string{"OK"}); ok {
		t.Fatal("matchToken found a token that is not there")
	}
}

func TestDialogStartsFromACleanBuffer(t *testing.T) {
	transport := &scriptedTransport{reply: okReply}

	modem := newTestModem(transport)
	// Bytes a previous dialog left behind must not answer this one: a stray
	// ERROR would abort the send before the modem was even asked.
	modem.pending = "\r\nERROR\r\n"

	if err := modem.Send(context.Background(), "628123456789", "PIN: 123456"); err != nil {
		t.Fatalf("leftover bytes from a previous dialog poisoned this one: %v", err)
	}
	if got := strings.Join(transport.written(), ""); !strings.Contains(got, "AT+CMGS=") {
		t.Fatal("the modem was never asked to send the message")
	}
}
