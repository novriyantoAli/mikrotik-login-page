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

// Send delivers one SMS. It implements sms.Sender.
func (m *Modem) Send(ctx context.Context, phoneNumber, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	transport, err := m.ensureTransport()
	if err != nil {
		return err
	}

	if err := m.dialog(transport, phoneNumber, message); err != nil {
		// The device is in an unknown state after a failed dialog; close it so
		// the next attempt starts a clean session instead of parsing stale output.
		m.close()
		m.logger.Error("modem dialog failed",
			zap.String("device", m.settings.Device),
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

	// Echo off keeps the parser from reading its own commands back as replies.
	// A modem that does not answer this is not fatal: the first real command
	// below reports the actual problem.
	_, _ = m.command(transport, "ATE0")
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

// dialog runs one full send: text mode, GSM charset, recipient prompt, message,
// and the +CMGS acknowledgement.
func (m *Modem) dialog(transport Transport, phoneNumber, message string) error {
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
	if _, err := m.expect(transport, ">", "ERROR"); err != nil {
		return err
	}

	// Ctrl-Z terminates the message body and submits it.
	if err := m.write(transport, message+"\x1a"); err != nil {
		return err
	}
	if _, err := m.expect(transport, "+CMGS:", "ERROR", "+CMS ERROR"); err != nil {
		return err
	}

	// The modem still answers OK after the reference number.
	if _, err := m.expect(transport, "OK", "ERROR"); err != nil {
		return err
	}
	return nil
}

// command writes one AT command and waits for its OK.
func (m *Modem) command(transport Transport, command string) (string, error) {
	if err := m.write(transport, command+"\r"); err != nil {
		return "", err
	}
	return m.expect(transport, "OK", "ERROR", "+CMS ERROR")
}

func (m *Modem) write(transport Transport, payload string) error {
	if _, err := transport.Write([]byte(payload)); err != nil {
		return fmt.Errorf("write to modem: %w", err)
	}
	return nil
}

// expect reads until one of the tokens appears and reports which one won. A
// timeout is an error, never a silent success. Bytes read past the matching token
// stay buffered for the next call.
func (m *Modem) expect(transport Transport, tokens ...string) (string, error) {
	deadline := time.Now().Add(m.settings.CommandTimeout)
	chunk := make([]byte, 64)

	for {
		if answer, ok := takeToken(&m.pending, tokens); ok {
			if isFailureAnswer(answer) {
				return "", fmt.Errorf("modem answered %s", strings.TrimSpace(answer))
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
					m.settings.CommandTimeout, strings.TrimSpace(m.pending))
			}
			return "", fmt.Errorf("no modem response within %s: %w", m.settings.CommandTimeout, err)
		}
	}
}

// isFailureAnswer reports whether the modem refused the command. It is a map
// lookup rather than a string comparison so the AT vocabulary stays in one place.
func isFailureAnswer(answer string) bool {
	return failureAnswers[answer]
}

var failureAnswers = map[string]bool{"ERROR": true, "+CMS ERROR": true}

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
