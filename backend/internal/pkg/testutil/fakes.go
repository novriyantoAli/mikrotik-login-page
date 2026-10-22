package testutil

import (
	"context"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
)

// TestDoubles bundles the fakes injected in place of the real vendor edges: the
// payment gateway, the SMS modem and the FreeRADIUS writer, plus a frozen clock.
type TestDoubles struct {
	Clock   *FakeClock
	Gateway *FakeGateway
	SMS     *FakeSMSSender
	Radius  *FakeRadiusWriter
}

// NewTestDoubles builds a fresh set of fakes for one test.
func NewTestDoubles() *TestDoubles {
	return &TestDoubles{
		Clock:   NewFakeClock(),
		Gateway: NewFakeGateway(),
		SMS:     NewFakeSMSSender(),
		Radius:  NewFakeRadiusWriter(),
	}
}

// FxOption provides the fakes both as concrete types (so tests can inspect what
// was recorded) and as the ports the domains depend on.
func (d *TestDoubles) FxOption() fx.Option {
	return fx.Options(
		fx.Provide(
			func() clock.Clock { return d.Clock },
			func() *FakeClock { return d.Clock },
			func() *FakeGateway { return d.Gateway },
			func(f *FakeGateway) payment.Gateway { return f },
			func() *FakeSMSSender { return d.SMS },
			func(f *FakeSMSSender) notification.Sender { return f },
			func() *FakeRadiusWriter { return d.Radius },
			func(f *FakeRadiusWriter) provisioning.Writer { return f },
		),
	)
}

// FakeGateway is a payment.Gateway that records calls and returns canned data.
type FakeGateway struct {
	mu sync.Mutex

	CreateRequests []payment.CreateTransactionRequest
	Callbacks      []payment.Callback
	Statuses       map[string]payment.PaymentStatus

	CreateErr   error
	StatusErr   error
	CallbackErr error
}

// NewFakeGateway builds an empty fake gateway.
func NewFakeGateway() *FakeGateway {
	return &FakeGateway{Statuses: make(map[string]payment.PaymentStatus)}
}

// LastCreateRequest returns the most recent request, or false when none was
// made.
func (g *FakeGateway) LastCreateRequest() (payment.CreateTransactionRequest, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.CreateRequests) == 0 {
		return payment.CreateTransactionRequest{}, false
	}
	return g.CreateRequests[len(g.CreateRequests)-1], true
}

// CreateTransaction implements payment.Gateway.
func (g *FakeGateway) CreateTransaction(
	_ context.Context,
	req payment.CreateTransactionRequest,
) (*payment.CreateTransactionResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.CreateErr != nil {
		return nil, g.CreateErr
	}

	g.CreateRequests = append(g.CreateRequests, req)
	return &payment.CreateTransactionResponse{
		Reference: req.Reference,
		PayCode:   "8808812345678901",
		PayURL:    "https://sandbox.tripay.example/pay/" + req.Reference,
		QRURL:     "https://sandbox.tripay.example/qr/" + req.Reference,
		ExpiresAt: req.ExpiresAt,
	}, nil
}

// GetStatus implements payment.Gateway.
func (g *FakeGateway) GetStatus(_ context.Context, reference string) (*payment.PaymentStatus, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.StatusErr != nil {
		return nil, g.StatusErr
	}
	status, ok := g.Statuses[reference]
	if !ok {
		return &payment.PaymentStatus{Reference: reference, Status: payment.StatusUnpaid}, nil
	}
	return &status, nil
}

// VerifyCallback implements payment.Gateway.
func (g *FakeGateway) VerifyCallback(raw []byte, _ string) (*payment.Callback, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.CallbackErr != nil {
		return nil, g.CallbackErr
	}
	// The fake treats the raw body as the reference; the real adapter is the
	// only place that knows a vendor payload shape.
	callback := payment.Callback{Reference: string(raw), Status: payment.StatusPaid}
	g.Callbacks = append(g.Callbacks, callback)
	return &callback, nil
}

// FakeSMSSender records every message instead of touching a modem.
type FakeSMSSender struct {
	mu sync.Mutex

	Sent []SentSMS
	Err  error
}

// SentSMS is one recorded delivery attempt.
type SentSMS struct {
	PhoneNumber string
	Message     string
}

// NewFakeSMSSender builds an empty fake sender.
func NewFakeSMSSender() *FakeSMSSender { return &FakeSMSSender{} }

// Send implements notification.Sender.
func (s *FakeSMSSender) Send(_ context.Context, phoneNumber, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Err != nil {
		return s.Err
	}
	s.Sent = append(s.Sent, SentSMS{PhoneNumber: phoneNumber, Message: message})
	return nil
}

// Messages returns a copy of everything sent so far.
func (s *FakeSMSSender) Messages() []SentSMS {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SentSMS(nil), s.Sent...)
}

// FakeRadiusWriter records the account state it was asked to apply.
type FakeRadiusWriter struct {
	mu sync.Mutex

	Applied []provisioning.Account
	Err     error
}

// NewFakeRadiusWriter builds an empty fake writer.
func NewFakeRadiusWriter() *FakeRadiusWriter { return &FakeRadiusWriter{} }

// Apply implements provisioning.Writer.
func (w *FakeRadiusWriter) Apply(_ context.Context, account provisioning.Account) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.Err != nil {
		return w.Err
	}
	w.Applied = append(w.Applied, account)
	return nil
}

// Accounts returns a copy of everything applied so far.
func (w *FakeRadiusWriter) Accounts() []provisioning.Account {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]provisioning.Account(nil), w.Applied...)
}

// Now returns the frozen clock time (convenience for assertions).
func (d *TestDoubles) Now() time.Time { return d.Clock.Now() }
