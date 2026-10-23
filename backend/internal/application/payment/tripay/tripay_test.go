package tripay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
)

const testPrivateKey = "TESTSECRET-PrivateKey-0123456789"

func newTestAdapter() *Adapter {
	return New(Settings{
		APIKey:       "TESTSECRET-ApiKey",
		PrivateKey:   testPrivateKey,
		MerchantCode: "T0001",
		BaseURL:      "https://sandbox.invalid/api-sandbox",
		CallbackURL:  "https://app.example/api/payments/callback",
		Timeout:      2 * time.Second,
	}, zap.NewNop())
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func sign(raw []byte) string {
	mac := hmac.New(sha256.New, []byte(testPrivateKey))
	mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

// TestVerifyCallback_AcceptsAProperlySignedPayload covers the one thing that
// makes a callback trustworthy: an HMAC over the raw bytes, checked in constant
// time. The payload is a vendor-shaped fixture, no network involved.
func TestVerifyCallback_AcceptsAProperlySignedPayload(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "callback.paid.json")
	callback, err := newTestAdapter().VerifyCallback(raw, sign(raw))
	if err != nil {
		t.Fatalf("VerifyCallback: %v", err)
	}

	if callback.Reference != "TRX-0123456789ABCDEF" {
		t.Fatalf("reference = %q, want our merchant_ref", callback.Reference)
	}
	if callback.Status != payment.StatusPaid {
		t.Fatalf("status = %q, want paid", callback.Status)
	}
	if callback.Amount != 25000 {
		t.Fatalf("amount = %d, want 25000", callback.Amount)
	}
	if callback.PaidAt == nil || callback.PaidAt.UTC().Format(time.RFC3339) != "2026-10-23T04:05:06Z" {
		t.Fatalf("paid_at = %v", callback.PaidAt)
	}
}

func TestVerifyCallback_RejectsAWrongSignature(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "callback.paid.json")
	forged := sign([]byte(`{"success":true,"data":{"merchant_ref":"TRX-other","status":"PAID"}}`))

	_, err := newTestAdapter().VerifyCallback(raw, forged)
	if !errors.Is(err, payment.ErrSignatureMismatch) {
		t.Fatalf("err = %v, want payment.ErrSignatureMismatch", err)
	}
}

func TestVerifyCallback_RejectsAnEmptySignature(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "callback.paid.json")
	if _, err := newTestAdapter().VerifyCallback(raw, ""); !errors.Is(err, payment.ErrSignatureMismatch) {
		t.Fatalf("err = %v, want payment.ErrSignatureMismatch", err)
	}
}

func TestVerifyCallback_RejectsTamperedPayloadWithTheOriginalSignature(t *testing.T) {
	t.Parallel()

	raw := readFixture(t, "callback.paid.json")
	signature := sign(raw)

	tampered := []byte(`{"success":true,"event":"payment_status","data":{"reference":"T1","merchant_ref":"TRX-0123456789ABCDEF","amount":1000,"status":"PAID"}}`)
	if _, err := newTestAdapter().VerifyCallback(tampered, signature); !errors.Is(err, payment.ErrSignatureMismatch) {
		t.Fatalf("a payload changed after signing must be rejected, got %v", err)
	}
}

func TestVerifyCallback_MapsEveryVendorStatusToADomainState(t *testing.T) {
	t.Parallel()

	cases := map[string]payment.Status{
		"UNPAID":  payment.StatusUnpaid,
		"PAID":    payment.StatusPaid,
		"EXPIRED": payment.StatusExpired,
		"FAILED":  payment.StatusFailed,
		"REFUND":  payment.StatusFailed,
		"paid":    payment.StatusPaid,
	}
	for vendor, want := range cases {
		raw := []byte(`{"success":true,"event":"payment_status","data":{"reference":"T1",` +
			`"merchant_ref":"TRX-1","amount":5000,"status":"` + vendor + `"}}`)

		callback, err := newTestAdapter().VerifyCallback(raw, sign(raw))
		if err != nil {
			t.Fatalf("%s: VerifyCallback: %v", vendor, err)
		}
		if callback.Status != want {
			t.Fatalf("%s mapped to %q, want %q", vendor, callback.Status, want)
		}
	}
}

func TestVerifyCallback_UnknownStatusIsAnErrorNotAGuess(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"success":true,"event":"payment_status","data":{"reference":"T1",` +
		`"merchant_ref":"TRX-1","amount":5000,"status":"CHARGEBACK"}}`)

	if _, err := newTestAdapter().VerifyCallback(raw, sign(raw)); !errors.Is(err, ErrUnknownStatus) {
		t.Fatalf("err = %v, want ErrUnknownStatus", err)
	}
}

func TestVerifyCallback_PayloadWithoutOurReferenceIsRejected(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"success":true,"event":"payment_status","data":{"reference":"T1","amount":5000,` +
		`"status":"PAID"}}`)

	if _, err := newTestAdapter().VerifyCallback(raw, sign(raw)); !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
}

func TestParseCreateResponse_MapsTheInstructionsAndTheExpiry(t *testing.T) {
	t.Parallel()

	var envelope createEnvelope
	if err := decodeFixture(t, "create.response.json", &envelope); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	fallback := time.Date(2026, time.October, 23, 5, 0, 0, 0, time.UTC)
	created := parseCreateResponse(envelope.Data, fallback)

	if created.Reference != "TRX-0123456789ABCDEF" {
		t.Fatalf("reference = %q", created.Reference)
	}
	if created.ChannelName != "BRIVA" || created.PayCode != "8808812345678901" {
		t.Fatalf("instructions lost: %+v", created)
	}
	if len(created.Instructions) != 1 || len(created.Instructions[0].Steps) != 2 {
		t.Fatalf("instructions = %+v", created.Instructions)
	}
	if created.ExpiresAt.Format(time.RFC3339) != "2026-10-23T05:00:00Z" {
		t.Fatalf("expires_at = %s, want the vendor's expired_time", created.ExpiresAt)
	}
}

func TestParseCreateResponse_KeepsTheConfiguredExpiryWhenTheVendorOmitsIt(t *testing.T) {
	t.Parallel()

	fallback := time.Date(2026, time.October, 23, 5, 0, 0, 0, time.UTC)
	created := parseCreateResponse(createData{MerchantRef: "TRX-1"}, fallback)

	if !created.ExpiresAt.Equal(fallback) {
		t.Fatalf("expires_at = %s, want %s", created.ExpiresAt, fallback)
	}
}

func TestParseChannels_KeepsActiveChannelsAndTheirFees(t *testing.T) {
	t.Parallel()

	var envelope channelsEnvelope
	if err := decodeFixture(t, "channels.response.json", &envelope); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	channels := parseChannels(envelope.Data)
	if len(channels) != 2 {
		t.Fatalf("got %d channels, want the 2 active ones", len(channels))
	}
	if channels[0].Code != "BRIVA" || channels[0].FeeCustomer.FlatIDR != 2500 {
		t.Fatalf("first channel = %+v", channels[0])
	}
	if channels[1].Code != "QRIS" || channels[1].FeeCustomer.Percent != 0.7 {
		t.Fatalf("second channel = %+v", channels[1])
	}
}

// decodeFixture reads a vendor-shaped fixture the way the adapter would read a
// response body. It lives in the test so no production code knows about
// testdata.
func decodeFixture(t *testing.T, name string, target any) error {
	t.Helper()

	raw := readFixture(t, name)
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return nil
}
