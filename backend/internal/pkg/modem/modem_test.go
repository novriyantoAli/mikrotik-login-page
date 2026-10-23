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
)

// scriptedTransport answers every command with a canned response and records
// what was written, so the AT dialog is tested without hardware.
type scriptedTransport struct {
	mu      sync.Mutex
	pending []byte
	writes  []string
	reply   func(command string) string
	closed  bool
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

func TestMatchTokenReportsTheEarliestAnswer(t *testing.T) {
	token, ok := matchToken("\r\nERROR\r\nOK\r\n", []string{"OK", "ERROR", "+CMS ERROR"})
	if !ok || token != "ERROR" {
		t.Fatalf("matchToken = (%q, %v), want ERROR", token, ok)
	}

	if _, ok := matchToken("nothing yet", []string{"OK"}); ok {
		t.Fatal("matchToken found a token that is not there")
	}
}
