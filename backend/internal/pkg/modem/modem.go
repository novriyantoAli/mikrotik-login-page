// Package modem implements sms.Sender for a Wavecom modem attached on
// a serial device, speaking AT commands (ADR 0003). It is the only code in the
// system that touches the device, and it serializes every dialog behind one
// mutex so two sends can never interleave commands (ADR 0010).
package modem

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
)

// Transport is the byte stream the AT dialog runs on: a serial port in
// production, a canned-response buffer in tests.
type Transport interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	SetReadDeadline(t time.Time) error
	Close() error
}

// Settings are the modem knobs the adapter needs.
type Settings struct {
	// Device is the serial device path (e.g. /dev/ttyUSB0).
	Device string
	// Baud is the serial speed the modem expects.
	Baud int
	// CommandTimeout bounds one AT command round trip.
	CommandTimeout time.Duration
	// SubmitTimeout bounds the modem's hand-off of the message body to the
	// network. It is longer than CommandTimeout on purpose: submitting a message
	// is not an AT round trip, and a modem that has to wait for the network can
	// take well over ten seconds before it answers +CMGS (ADR 0022). A zero value
	// falls back to CommandTimeout.
	SubmitTimeout time.Duration
}

// Opener opens the transport; tests replace it instead of touching hardware.
type Opener func(Settings) (Transport, error)

// Modem sends SMS through the serial device. The transport is opened lazily and
// reopened after a failure, so a worker that started before the modem was
// plugged in recovers on the next attempt instead of needing a restart.
type Modem struct {
	mu       sync.Mutex
	settings Settings
	opener   Opener
	logger   *zap.Logger

	transport Transport
	// pending holds bytes a previous expect already read past its token. A modem
	// often answers "+CMGS" and "OK" in one burst, so the leftover must survive
	// into the next expect instead of being dropped.
	pending string
}

// New builds the modem sender; Fx injects the settings and provides it as
// sms.Sender.
func New(settings Settings, logger *zap.Logger) *Modem {
	return newWithOpener(settings, openTransport, logger)
}

// newWithOpener builds a modem on an explicit transport opener.
func newWithOpener(settings Settings, opener Opener, logger *zap.Logger) *Modem {
	return &Modem{settings: settings, opener: opener, logger: logger}
}

// NewWithTransport builds a modem on an already-open transport. Tests use it to
// exercise the AT dialog without a device.
func NewWithTransport(settings Settings, transport Transport, logger *zap.Logger) *Modem {
	return &Modem{
		settings:  settings,
		opener:    func(Settings) (Transport, error) { return transport, nil },
		logger:    logger,
		transport: transport,
	}
}

var _ sms.Sender = (*Modem)(nil)

const (
	// escape is the AT escape character: it cancels a pending message prompt.
	escape = byte(0x1b)
	// quietWindow is how long silence on the port marks the end of stale output.
	quietWindow = 200 * time.Millisecond
)

// Send delivers one SMS. It implements sms.Sender.
func (m *Modem) Send(ctx context.Context, phoneNumber, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	// The dialog's duration is part of the diagnosis: a refusal after twenty
	// seconds of network wait is a different fault from one that came back at
	// once (ADR 0023).
	started := time.Now()

	// A body outside the GSM alphabet, or longer than one text-mode segment, is
	// refused by every modem every time. Check it before the device is touched so
	// the caller records a permanent cause instead of a bare "ERROR" that looks
	// like a hardware fault.
	if err := sms.CheckText(message); err != nil {
		m.logger.Error("sms body cannot be sent",
			zap.String("device", m.settings.Device),
			zap.Error(err),
		)
		return err
	}

	transport, err := m.ensureTransport()
	if err != nil {
		return err
	}

	if err := m.dialog(transport, phoneNumber, message); err != nil {
		// A rejected body leaves the modem waiting at the ">" prompt, and that
		// prompt outlives the port: the next session's commands would be swallowed
		// as message text. Cancel it, then close so the next attempt is clean.
		m.abort(transport)
		m.close()
		m.logger.Error("modem dialog failed",
			zap.String("device", m.settings.Device),
			zap.Duration("elapsed", time.Since(started)),
			zap.Error(err),
		)
		return fmt.Errorf("send sms: %w", err)
	}

	m.logger.Info("modem accepted sms")
	return nil
}

func (m *Modem) ensureTransport() (Transport, error) {
	if m.transport != nil {
		return m.transport, nil
	}

	transport, err := m.opener(m.settings)
	if err != nil {
		return nil, fmt.Errorf("open modem %s: %w", m.settings.Device, err)
	}
	m.transport = transport

	// A crash, or a rejected body in an earlier session, can leave the modem at the
	// ">" prompt. ESC closes it (and is ignored at the AT prompt), so the ATE0
	// below is not read back as message text.
	m.abort(transport)

	// Echo off keeps the parser from reading its own commands back as replies.
	// A modem that does not answer this is not fatal: the first real command
	// below reports the actual problem.
	_, _ = m.command(transport, "ATE0")

	// Numeric error reports replace a bare "ERROR" with the modem's own code —
	// "+CMS ERROR: 500" instead of the line that cost two nights (ADR 0023).
	// Best effort: a firmware that does not know the command still sends.
	if _, err := m.command(transport, "AT+CMEE=1"); err != nil {
		m.logger.Debug("modem rejected AT+CMEE, continuing", zap.Error(err))
	}
	return transport, nil
}

func (m *Modem) close() {
	if m.transport == nil {
		return
	}
	if err := m.transport.Close(); err != nil {
		m.logger.Warn("close modem", zap.Error(err))
	}
	m.transport = nil
	m.pending = ""
}

// abort drops whatever the modem is waiting for: ESC cancels a pending message,
// and is ignored at the AT prompt, so this is safe on a healthy session too. The
// answer is drained so it cannot answer a later command.
func (m *Modem) abort(transport Transport) {
	m.pending = ""
	if _, err := transport.Write([]byte{escape}); err != nil {
		m.logger.Debug("abort modem prompt", zap.Error(err))
		return
	}
	m.discard(transport)
}

// discard reads until the port falls quiet, dropping stale output (a prompt, an OK
// from an aborted command) that would otherwise be taken for the next answer.
func (m *Modem) discard(transport Transport) {
	chunk := make([]byte, 64)
	deadline := time.Now().Add(quietWindow)

	for {
		if err := transport.SetReadDeadline(deadline); err != nil {
			return
		}
		n, err := transport.Read(chunk)
		if n > 0 {
			deadline = time.Now().Add(quietWindow)
			continue
		}
		if err != nil {
			return
		}
	}
}

// dialog runs one full send: text mode, GSM charset, recipient prompt, message,
// and the +CMGS acknowledgement. Every failure names the command that was
// rejected, so a log line says which step broke instead of just "ERROR".
func (m *Modem) dialog(transport Transport, phoneNumber, message string) error {
	// Bytes a previous dialog read past its last token (a trailing "OK" is
	// common) must not be allowed to answer this dialog's first command.
	m.pending = ""

	if _, err := m.command(transport, "AT"); err != nil {
		return err
	}
	if _, err := m.command(transport, "AT+CMGF=1"); err != nil {
		return err
	}
	// Charset is best effort: a modem that already defaults to GSM rejects the
	// command on some firmware versions, and that must not fail a delivery.
	if _, err := m.command(transport, `AT+CSCS="GSM"`); err != nil {
		m.logger.Debug("modem rejected AT+CSCS, continuing", zap.Error(err))
	}

	if err := m.write(transport, fmt.Sprintf("AT+CMGS=%q\r", phoneNumber)); err != nil {
		return err
	}
	if _, err := m.expect(transport, ">", "ERROR", "+CME ERROR"); err != nil {
		return fmt.Errorf("AT+CMGS: %w", err)
	}

	// Ctrl-Z terminates the message body and submits it. This is the step a modem
	// answers with a bare ERROR when the body is longer than one segment or holds a
	// character the GSM alphabet cannot carry.
	if err := m.write(transport, message+"\x1a"); err != nil {
		return err
	}
	// The network, not the modem, answers this step: the modem only replies once
	// the SMSC has taken the message. That hand-off gets its own, longer budget
	// (ADR 0022).
	if _, err := m.expectWithin(transport, m.submitTimeout(), "+CMGS:", "ERROR", "+CMS ERROR", "+CME ERROR"); err != nil {
		return fmt.Errorf("AT+CMGS body: %w", err)
	}

	// The modem still answers OK after the reference number.
	if _, err := m.expect(transport, "OK", "ERROR", "+CME ERROR"); err != nil {
		return fmt.Errorf("AT+CMGS reference: %w", err)
	}
	return nil
}

// command writes one AT command and waits for its OK.
func (m *Modem) command(transport Transport, command string) (string, error) {
	if err := m.write(transport, command+"\r"); err != nil {
		return "", err
	}
	answer, err := m.expect(transport, "OK", "ERROR", "+CMS ERROR", "+CME ERROR")
	if err != nil {
		return "", fmt.Errorf("%s: %w", command, err)
	}
	return answer, nil
}

func (m *Modem) write(transport Transport, payload string) error {
	if _, err := transport.Write([]byte(payload)); err != nil {
		return fmt.Errorf("write to modem: %w", err)
	}
	return nil
}

// submitTimeout is the budget for the message hand-off, falling back to the
// command budget when a caller (a test, older wiring) did not set one.
func (m *Modem) submitTimeout() time.Duration {
	if m.settings.SubmitTimeout > 0 {
		return m.settings.SubmitTimeout
	}
	return m.settings.CommandTimeout
}

// expect reads until one of the tokens appears within the command budget.
func (m *Modem) expect(transport Transport, tokens ...string) (string, error) {
	return m.expectWithin(transport, m.settings.CommandTimeout, tokens...)
}

// expectWithin reads until one of the tokens appears and reports which one won. A
// timeout is an error, never a silent success. Bytes read past the matching token
// stay buffered for the next call. The timeout is a parameter because not every
// step is an AT round trip: the message hand-off is the network's business.
func (m *Modem) expectWithin(transport Transport, timeout time.Duration, tokens ...string) (string, error) {
	deadline := time.Now().Add(timeout)
	chunk := make([]byte, 64)

	for {
		if answer, ok := takeToken(&m.pending, tokens); ok {
			if isFailureAnswer(answer) {
				// A refusal's numeric cause arrives after the token ("+CMS ERROR", then
				// ": 500"), so the token can be matched before the code reaches the
				// buffer. Read the rest of the line first, or the one line the operator
				// needs is reported without its cause (ADR 0023).
				m.completeRefusal(transport, deadline)
				return "", fmt.Errorf("modem answered %s", failureCause(answer, &m.pending))
			}
			return answer, nil
		}

		if err := transport.SetReadDeadline(deadline); err != nil {
			return "", fmt.Errorf("set modem read deadline: %w", err)
		}

		n, err := transport.Read(chunk)
		if n > 0 {
			m.pending += string(chunk[:n])
			continue
		}
		if err != nil {
			if m.pending != "" {
				return "", fmt.Errorf("no modem response within %s (got %q)",
					timeout, strings.TrimSpace(m.pending))
			}
			return "", fmt.Errorf("no modem response within %s: %w", timeout, err)
		}
	}
}

// completeRefusal reads the remainder of a refusal line before that line is
// consumed. The token and its numeric cause can arrive in two reads on a slow
// link, and consuming the line at the token would both lose the code and leave the
// rest of it to answer the next command (ADR 0023). A port that stays quiet for
// the quiet window has no remainder to give, and the token is reported alone.
func (m *Modem) completeRefusal(transport Transport, deadline time.Time) {
	if strings.ContainsAny(m.pending, "\r\n") {
		return
	}

	lineDeadline := time.Now().Add(quietWindow)
	if lineDeadline.After(deadline) {
		lineDeadline = deadline
	}

	chunk := make([]byte, 64)
	for !strings.ContainsAny(m.pending, "\r\n") {
		if err := transport.SetReadDeadline(lineDeadline); err != nil {
			return
		}
		n, err := transport.Read(chunk)
		if n > 0 {
			m.pending += string(chunk[:n])
			continue
		}
		if err != nil {
			return
		}
	}
}

// isFailureAnswer reports whether the modem refused the command. It is a map
// lookup rather than a string comparison so the AT vocabulary stays in one place.
func isFailureAnswer(answer string) bool {
	return failureAnswers[answer]
}

var failureAnswers = map[string]bool{"ERROR": true, "+CMS ERROR": true, "+CME ERROR": true}

// failureCause renders a refusal with the modem's own numeric cause when it sent
// one: "+CMS ERROR: 500" rather than "ERROR" (ADR 0023). The rest of that line
// belongs to the refusal, so it is consumed here instead of answering the next
// command. The code is reported as the modem wrote it: 3GPP TS 27.007 numbers are
// stable, while a translation into prose would only be a guess per firmware.
func failureCause(token string, buffer *string) string {
	rest := *buffer
	end := strings.IndexAny(rest, "\r\n")
	if end < 0 {
		end = len(rest)
	}
	*buffer = rest[end:]

	detail := strings.TrimSpace(rest[:end])
	switch {
	case detail == "":
		return token
	case strings.HasPrefix(detail, ":"):
		return token + detail
	default:
		return token + " " + detail
	}
}

// takeToken removes everything up to and including the earliest matching token
// and reports it.
func takeToken(buffer *string, tokens []string) (string, bool) {
	token, ok := matchToken(*buffer, tokens)
	if !ok {
		return "", false
	}
	index := strings.Index(*buffer, token)
	*buffer = (*buffer)[index+len(token):]
	return token, true
}

// matchToken finds the first occurrence of any token, so an early ERROR is not
// masked by an OK that happens to come later.
func matchToken(buffer string, tokens []string) (string, bool) {
	bestIndex := -1
	var best string
	for _, token := range tokens {
		index := strings.Index(buffer, token)
		if index == -1 {
			continue
		}
		if bestIndex == -1 || index < bestIndex {
			bestIndex, best = index, token
		}
	}
	return best, bestIndex != -1
}
