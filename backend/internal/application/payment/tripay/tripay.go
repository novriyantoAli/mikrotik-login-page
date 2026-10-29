// Package tripay is the only code in this repository that knows Tripay exists:
// merchant codes, HMAC signatures, channel identifiers and the vendor's status
// vocabulary all stop here (ADR 0008). Everything it returns is this system's
// vocabulary, so the Transaction service can be tested with a fake gateway and
// never imports an HTTP client or a vendor type.
package tripay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
)

// Errors the adapter reports. They are sentinels so the caller can tell an
// unauthenticated callback (security event) from a vendor rejection.
var (
	// ErrUnknownStatus means the vendor used a status this system does not map;
	// it is never guessed into a domain state.
	ErrUnknownStatus = errors.New("tripay: unknown status")
	// ErrRejected wraps every non-2xx or unsuccessful vendor answer.
	ErrRejected = errors.New("tripay: request rejected")
)

// maxBodyBytes bounds one vendor response. A gateway answer is small; reading
// more would only give a broken server a way to exhaust memory.
const maxBodyBytes = 1 << 20

// merchantCustomerName and merchantCustomerEmail are the placeholder customer
// fields Tripay requires. The only real identity this system stores is the Phone
// Number, which is passed through separately.
const (
	merchantCustomerName  = "Pembeli Hotspot"
	merchantCustomerEmail = "noreply@hotspot.local"
)

// Settings is the adapter's slice of deployment configuration.
type Settings struct {
	APIKey       string
	PrivateKey   string
	MerchantCode string
	BaseURL      string
	CallbackURL  string
	ReturnURL    string
	Timeout      time.Duration
}

// Adapter implements payment.Gateway on top of Tripay's HTTP API.
type Adapter struct {
	settings Settings
	client   *http.Client
	logger   *zap.Logger
}

var _ payment.Gateway = (*Adapter)(nil)

// New builds the adapter. Missing credentials do not fail startup: the stack
// must boot for an operator to see health, and checkout reports the failure when
// it actually calls the vendor.
func New(settings Settings, logger *zap.Logger) *Adapter {
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if settings.APIKey == "" || settings.PrivateKey == "" || settings.MerchantCode == "" {
		logger.Warn("tripay credentials are not configured; checkout will fail until they are")
	}

	return &Adapter{
		settings: settings,
		client:   &http.Client{Timeout: timeout},
		logger:   logger,
	}
}

// CreateTransaction opens a closed-payment channel for one Transaction.
func (a *Adapter) CreateTransaction(
	ctx context.Context,
	req payment.CreateTransactionRequest,
) (*payment.CreateTransactionResponse, error) {
	form := url.Values{}
	form.Set("method", req.Channel)
	form.Set("merchant_ref", req.Reference)
	form.Set("amount", strconv.FormatInt(req.Amount, 10))
	form.Set("customer_name", merchantCustomerName)
	form.Set("customer_email", merchantCustomerEmail)
	form.Set("customer_phone", req.PhoneNumber)
	form.Set("callback_url", a.settings.CallbackURL)
	form.Set("return_url", a.settings.ReturnURL)
	form.Set("expired_time", strconv.FormatInt(req.ExpiresAt.Unix(), 10))
	// Tripay refuses a closed-payment request without this field: the vendor answers
	// 400 "Undefined parameter signature", which is not a credential problem but a
	// missing one. It signs the same amount string that is sent below.
	form.Set("signature", a.createSignature(req.Reference, form.Get("amount")))

	// Tripay reads order_items as PHP-style nested form fields, not as a JSON
	// string: a JSON array in this field is answered 400 "order items must be an
	// array", so each property of the single line item is its own key.
	form.Set("order_items[0][sku]", req.Reference)
	form.Set("order_items[0][name]", req.PackageName)
	form.Set("order_items[0][price]", strconv.FormatInt(req.Amount, 10))
	form.Set("order_items[0][quantity]", "1")

	request, err := a.newRequest(ctx, http.MethodPost, "/transaction/create",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var envelope createEnvelope
	if err := a.do(request, &envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		return nil, fmt.Errorf("%w: %s", ErrRejected, envelope.Message)
	}

	created := parseCreateResponse(envelope.Data, req.ExpiresAt)
	a.logger.Info("tripay transaction created",
		zap.String("reference", req.Reference),
		zap.String("channel", req.Channel),
	)
	return created, nil
}

// GetStatus reads one payment's current state.
func (a *Adapter) GetStatus(ctx context.Context, reference string) (*payment.PaymentStatus, error) {
	request, err := a.newRequest(ctx, http.MethodGet,
		"/transaction/detail?reference="+url.QueryEscape(reference), nil)
	if err != nil {
		return nil, err
	}

	var envelope detailEnvelope
	if err := a.do(request, &envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		return nil, fmt.Errorf("%w: %s", ErrRejected, envelope.Message)
	}

	status, err := mapStatus(envelope.Data.Status)
	if err != nil {
		return nil, err
	}

	return &payment.PaymentStatus{
		Reference: envelope.Data.MerchantRef,
		Status:    status,
		Amount:    envelope.Data.Amount,
		PaidAt:    unixTime(envelope.Data.PaidAt),
	}, nil
}

// VerifyCallback authenticates a settlement notification before anything in it
// is believed. The signature covers the raw payload bytes, so the caller must
// pass the body exactly as it arrived.
func (a *Adapter) VerifyCallback(raw []byte, signature string) (*payment.Callback, error) {
	if !a.validSignature(raw, signature) {
		return nil, payment.ErrSignatureMismatch
	}

	var payload callbackPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode tripay callback: %w", err)
	}
	if !payload.Success {
		return nil, fmt.Errorf("%w: callback is not successful", ErrRejected)
	}

	status, err := mapStatus(payload.Data.Status)
	if err != nil {
		return nil, err
	}

	reference := payload.Data.MerchantRef
	if reference == "" {
		// The vendor's own reference must never be silently used as ours: an
		// unexpected payload is logged by the caller, not reinterpreted here.
		return nil, fmt.Errorf("%w: callback carries no merchant_ref", ErrRejected)
	}

	return &payment.Callback{
		Reference: reference,
		Status:    status,
		Amount:    payload.Data.Amount,
		PaidAt:    unixTime(payload.Data.PaidAt),
	}, nil
}

// Channels lists the closed-payment methods the merchant may offer.
func (a *Adapter) Channels(ctx context.Context) ([]payment.Channel, error) {
	request, err := a.newRequest(ctx, http.MethodGet, "/merchant/payment-channel", nil)
	if err != nil {
		return nil, err
	}

	var envelope channelsEnvelope
	if err := a.do(request, &envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		return nil, fmt.Errorf("%w: %s", ErrRejected, envelope.Message)
	}

	return parseChannels(envelope.Data), nil
}

// createSignature signs a closed-payment request. Tripay expects the hex
// HMAC-SHA256 of merchant code + merchant reference + amount, keyed with the
// private key (no channel, unlike some other vendors). The signed amount must be
// the exact string sent, or the vendor rejects the request.
func (a *Adapter) createSignature(reference, amount string) string {
	mac := hmac.New(sha256.New, []byte(a.settings.PrivateKey))
	mac.Write([]byte(a.settings.MerchantCode + reference + amount))
	return hex.EncodeToString(mac.Sum(nil))
}

// validSignature compares the callback signature with the HMAC-SHA256 of the
// raw payload, in constant time.
func (a *Adapter) validSignature(raw []byte, signature string) bool {
	if signature == "" || a.settings.PrivateKey == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(a.settings.PrivateKey))
	mac.Write(raw)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(strings.ToLower(strings.TrimSpace(signature))))
}

func (a *Adapter) newRequest(
	ctx context.Context,
	method string,
	path string,
	body io.Reader,
) (*http.Request, error) {
	target := strings.TrimSuffix(a.settings.BaseURL, "/") + path

	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("build tripay request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+a.settings.APIKey)
	request.Header.Set("Accept", "application/json")
	return request, nil
}

func (a *Adapter) do(request *http.Request, target any) error {
	response, err := a.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrRejected, request.URL.Path, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("read tripay response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: %s answered %d: %s",
			ErrRejected, request.URL.Path, response.StatusCode, truncate(body))
	}

	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode tripay response: %w", err)
	}
	return nil
}

func truncate(body []byte) string {
	const limit = 200
	if len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "…"
}

func unixTime(value *int64) *time.Time {
	if value == nil || *value <= 0 {
		return nil
	}
	moment := time.Unix(*value, 0).UTC()
	return &moment
}
