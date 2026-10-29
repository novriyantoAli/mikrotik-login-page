package testutil

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
)

// TestDoubles bundles the fakes injected in place of the real vendor edges: the
// payment gateway, the SMS modem, the FreeRADIUS writer and reader, plus a
// frozen clock.
type TestDoubles struct {
	Clock        *FakeClock
	Gateway      *FakeGateway
	SMS          *FakeSMSSender
	Radius       *FakeRadiusWriter
	RadiusReader *FakeRadiusReader
}

// NewTestDoubles builds a fresh set of fakes for one test.
func NewTestDoubles() *TestDoubles {
	return &TestDoubles{
		Clock:        NewFakeClock(),
		Gateway:      NewFakeGateway(),
		SMS:          NewFakeSMSSender(),
		Radius:       NewFakeRadiusWriter(),
		RadiusReader: NewFakeRadiusReader(),
	}
}

// DoublesOption adjusts which fakes a harness injects.
type DoublesOption func(*doublesSettings)

type doublesSettings struct {
	realRadiusReader bool
	realRadiusWriter bool
}

// WithoutRadiusReaderFake keeps the production FreeRADIUS MySQL adapter instead
// of the in-memory fake, so a test can assert the degraded database path end to
// end.
func WithoutRadiusReaderFake() DoublesOption {
	return func(settings *doublesSettings) { settings.realRadiusReader = true }
}

// WithoutRadiusWriterFake keeps the production FreeRADIUS MySQL writer, for the
// one test that proves the Expand arithmetic against a real database (ADR 0016).
func WithoutRadiusWriterFake() DoublesOption {
	return func(settings *doublesSettings) { settings.realRadiusWriter = true }
}

// FxOption provides the fakes both as concrete types (so tests can inspect what
// was recorded) and as the ports the domains depend on. The FreeRADIUS read
// port is decorated rather than provided because the production graph already
// offers the real MySQL adapter.
func (d *TestDoubles) FxOption(options ...DoublesOption) fx.Option {
	var settings doublesSettings
	for _, option := range options {
		option(&settings)
	}

	provided := []fx.Option{
		fx.Provide(
			func() clock.Clock { return d.Clock },
			func() *FakeClock { return d.Clock },
			func() *FakeGateway { return d.Gateway },
			func() *FakeSMSSender { return d.SMS },
			func(f *FakeSMSSender) sms.Sender { return f },
			func() *FakeRadiusWriter { return d.Radius },
			func() *FakeRadiusReader { return d.RadiusReader },
		),
	}
	// The production graph provides the Tripay adapter behind payment.Gateway, so
	// the fake takes its place rather than competing with it.
	provided = append(provided, fx.Decorate(
		func(_ payment.Gateway, fake *FakeGateway) payment.Gateway { return fake },
	))
	if !settings.realRadiusWriter {
		// Provisioning has a production MySQL writer now, so the fake takes its place
		// instead of competing with it (ADR 0016).
		provided = append(provided, fx.Decorate(
			func(_ provisioning.Writer, fake *FakeRadiusWriter) provisioning.Writer { return fake },
		))
	}
	if !settings.realRadiusReader {
		// Decorate rather than Provide: the production graph already offers the
		// MySQL adapter, and the fake has to take its place, not compete with it.
		provided = append(provided, fx.Decorate(
			func(_ provisioning.ExpirationReader, fake *FakeRadiusReader) provisioning.ExpirationReader {
				return fake
			},
		))
	}
	return fx.Options(provided...)
}

// FakeGateway is a payment.Gateway that records calls and returns canned data.
type FakeGateway struct {
	mu sync.Mutex

	CreateRequests []payment.CreateTransactionRequest
	Callbacks      []payment.Callback
	Statuses       map[string]payment.PaymentStatus
	// gatewayReferences maps the vendor's own reference (what the adapter hands
	// back on create) to this system's reference, so a test keeps seeding Statuses
	// by our reference while reconciliation still has to ask with the vendor's.
	gatewayReferences map[string]string
	// StatusReferences records every reference the reconciliation sweep asked
	// about, in order, so a test can prove a settled row is not polled again.
	StatusReferences []string
	ChannelList      []payment.Channel

	CreateErr   error
	StatusErr   error
	CallbackErr error
	ChannelsErr error
}

// NewFakeGateway builds an empty fake gateway with one channel of each shape a
// buyer can pick: a virtual account and a QRIS code.
func NewFakeGateway() *FakeGateway {
	return &FakeGateway{
		Statuses:          make(map[string]payment.PaymentStatus),
		gatewayReferences: make(map[string]string),
		ChannelList: []payment.Channel{
			{
				Code: "BRIVA", Name: "BRIVA", Group: "Virtual Account",
				FeeCustomer: payment.ChannelFee{FlatIDR: 2500},
			},
			{
				Code: "QRIS", Name: "QRIS", Group: "QRIS",
				FeeCustomer: payment.ChannelFee{Percent: 0.7},
			},
		},
	}
}

// SetChannelList replaces the channels the fake offers.
func (g *FakeGateway) SetChannelList(channels []payment.Channel) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ChannelList = channels
}

// Channels implements payment.Gateway.
func (g *FakeGateway) Channels(_ context.Context) ([]payment.Channel, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.ChannelsErr != nil {
		return nil, g.ChannelsErr
	}
	return append([]payment.Channel(nil), g.ChannelList...), nil
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
	// The fake vendor does not echo our reference the way Tripay's merchant_ref
	// does: it answers with its own id, and that is the one it must be asked
	// about later (a real vendor rejects the other one).
	gatewayReference := "GW-" + req.Reference
	g.gatewayReferences[gatewayReference] = req.Reference

	return &payment.CreateTransactionResponse{
		Reference:        req.Reference,
		GatewayReference: gatewayReference,
		ChannelName:      req.Channel,
		PayCode:          "8808812345678901",
		PayURL:           "https://sandbox.tripay.example/pay/" + req.Reference,
		QRString:         "00020101021226" + req.Reference,
		QRURL:            "https://sandbox.tripay.example/qr/" + req.Reference,
		Instructions: []payment.Instruction{
			{Title: "Cara pembayaran", Steps: []string{"Buka aplikasi bank", "Masukkan kode bayar"}},
		},
		ExpiresAt: req.ExpiresAt,
	}, nil
}

// GetStatus implements payment.Gateway. The argument is the gateway's own
// reference, as CreateTransactionResponse.GatewayReference returned it.
func (g *FakeGateway) GetStatus(_ context.Context, reference string) (*payment.PaymentStatus, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.StatusReferences = append(g.StatusReferences, reference)
	if g.StatusErr != nil {
		return nil, g.StatusErr
	}
	if own, ok := g.gatewayReferences[reference]; ok {
		reference = own
	}
	status, ok := g.Statuses[reference]
	if !ok {
		return &payment.PaymentStatus{Reference: reference, Status: payment.StatusUnpaid}, nil
	}
	return &status, nil
}

// StatusQueries returns the references the reconciliation sweep asked about.
func (g *FakeGateway) StatusQueries() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.StatusReferences...)
}

// ValidCallbackSignature is the signature the fake gateway accepts. It stands in
// for the vendor's HMAC: the real adapter is the only place that knows the real
// payload shape and the merchant secret.
const ValidCallbackSignature = "test-valid-signature"

// fakeCallbackPayload is the test double's notification body. It is deliberately
// this system's vocabulary, because no Tripay shape may leak into the service.
type fakeCallbackPayload struct {
	Reference string         `json:"reference"`
	Status    payment.Status `json:"status"`
	Amount    int64          `json:"amount"`
	PaidAt    *time.Time     `json:"paid_at"`
}

// VerifyCallback implements payment.Gateway.
func (g *FakeGateway) VerifyCallback(raw []byte, signature string) (*payment.Callback, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.CallbackErr != nil {
		return nil, g.CallbackErr
	}
	if signature != ValidCallbackSignature {
		return nil, payment.ErrSignatureMismatch
	}

	var payload fakeCallbackPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	callback := payment.Callback{
		Reference: payload.Reference,
		Status:    payload.Status,
		Amount:    payload.Amount,
		PaidAt:    payload.PaidAt,
	}
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

// Send implements sms.Sender.
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

// FakeRadiusReader answers Active Period reads from an in-memory map instead of
// FreeRADIUS MySQL. Keys are canonical Phone Numbers ("628…").
type FakeRadiusReader struct {
	mu sync.Mutex

	Expirations map[string]time.Time
	Err         error
}

// NewFakeRadiusReader builds an empty fake reader.
func NewFakeRadiusReader() *FakeRadiusReader {
	return &FakeRadiusReader{Expirations: make(map[string]time.Time)}
}

// SetExpiration records an Active Period end for one canonical Phone Number.
func (r *FakeRadiusReader) SetExpiration(phoneNumber string, expiresAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Expirations[phoneNumber] = expiresAt
}

// Expiration implements provisioning.ExpirationReader.
func (r *FakeRadiusReader) Expiration(
	_ context.Context,
	phoneNumber string,
) (provisioning.Expiration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.Err != nil {
		return provisioning.Expiration{}, r.Err
	}
	expiresAt, ok := r.Expirations[phoneNumber]
	if !ok {
		return provisioning.Expiration{}, nil
	}
	return provisioning.Expiration{ExpiresAt: expiresAt, Found: true}, nil
}
