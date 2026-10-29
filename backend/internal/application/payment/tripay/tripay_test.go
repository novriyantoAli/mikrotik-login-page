package tripay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

// A closed-payment request without a signature is answered 400 "Undefined
// parameter signature" — the production failure this test pins. The expected
// digest is hardcoded from Tripay's documented formula
// (merchant_code + merchant_ref + amount, HMAC-SHA256 with the private key) so a
// change to the concatenation order fails here instead of at the vendor.
func TestCreateTransaction_SignsTheRequestTripayExpects(t *testing.T) {
	t.Parallel()

	var sentForm map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transaction/create" {
			t.Errorf("path = %q, want /transaction/create", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		sentForm = r.PostForm

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "create.response.json"))
	}))
	defer server.Close()

	adapter := newTestAdapter()
	adapter.settings.BaseURL = server.URL

	expires := time.Unix(1792731600, 0).UTC()
	created, err := adapter.CreateTransaction(context.Background(), payment.CreateTransactionRequest{
		Reference:   "TRX-0123456789ABCDEF",
		Channel:     "BRIVA",
		Amount:      25000,
		PhoneNumber: "6281234567890",
		PackageName: "Paket Harian",
		ExpiresAt:   expires,
	})
	if err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}

	const wantSignature = "0b2fd870561dab1792b3a24380537941fcec9dcbf546e2f38cef93526f8438ea"
	if got := sentForm["signature"]; len(got) != 1 || got[0] != wantSignature {
		t.Fatalf("signature = %v, want %s", got, wantSignature)
	}
	// The signature covers these exact values, so they must be the ones sent.
	if got := sentForm["merchant_ref"]; len(got) != 1 || got[0] != "TRX-0123456789ABCDEF" {
		t.Fatalf("merchant_ref = %v", got)
	}
	if got := sentForm["amount"]; len(got) != 1 || got[0] != "25000" {
		t.Fatalf("amount = %v", got)
	}
	if got := sentForm["method"]; len(got) != 1 || got[0] != "BRIVA" {
		t.Fatalf("method = %v", got)
	}
	// Tripay reads the line items as nested form fields; a JSON string here is
	// answered "order items must be an array".
	for key, want := range map[string]string{
		"order_items[0][sku]":      "TRX-0123456789ABCDEF",
		"order_items[0][name]":     "Paket Harian",
		"order_items[0][price]":    "25000",
		"order_items[0][quantity]": "1",
	} {
		if got := sentForm[key]; len(got) != 1 || got[0] != want {
			t.Fatalf("%s = %v, want %q", key, got, want)
		}
	}
	if _, ok := sentForm["order_items"]; ok {
		t.Fatal("order_items sent as a single field; Tripay expects nested form fields")
	}
	if created.Reference != "TRX-0123456789ABCDEF" || created.PayCode == "" {
		t.Fatalf("created = %+v", created)
	}
	// The vendor's own id is the only handle a later status read accepts, so it
	// must survive the create call instead of being thrown away.
	if created.GatewayReference != "T000100231029000001" {
		t.Fatalf("gateway reference = %q, want the fixture's vendor id", created.GatewayReference)
	}
}

func TestCreateTransaction_ReportsAVendorRejection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"message":"Undefined parameter signature"}`))
	}))
	defer server.Close()

	adapter := newTestAdapter()
	adapter.settings.BaseURL = server.URL

	_, err := adapter.CreateTransaction(context.Background(), payment.CreateTransactionRequest{
		Reference: "TRX-1",
		Channel:   "BRIVA",
		Amount:    25000,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
}

// Tripay's detail endpoint is addressed by its own reference: a query by our
// merchant_ref is answered 400 "Undefined parameter: reference", which left
// reconciliation unable to see any payment (ADR 0018).
func TestGetStatus_AsksByTheVendorReference(t *testing.T) {
	t.Parallel()

	var askedReference string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transaction/detail" {
			t.Errorf("path = %q, want /transaction/detail", r.URL.Path)
		}
		askedReference = r.URL.Query().Get("reference")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"message":"Transaction found","data":{` +
			`"reference":"DEV-T40988414167T2HXK","merchant_ref":"TRX-0123456789ABCDEF",` +
			`"status":"PAID","amount":25000,"paid_at":1792731600}}`))
	}))
	defer server.Close()

	adapter := newTestAdapter()
	adapter.settings.BaseURL = server.URL

	status, err := adapter.GetStatus(context.Background(), "DEV-T40988414167T2HXK")
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if askedReference != "DEV-T40988414167T2HXK" {
		t.Fatalf("detail asked by %q, want the vendor reference", askedReference)
	}
	if status.Reference != "TRX-0123456789ABCDEF" {
		t.Fatalf("status reference = %q, want our merchant_ref", status.Reference)
	}
	if status.Status != payment.StatusPaid || status.Amount != 25000 {
		t.Fatalf("status = %+v, want paid 25000", status)
	}
	if status.PaidAt == nil || status.PaidAt.Unix() != 1792731600 {
		t.Fatalf("paid_at = %v, want the vendor's timestamp", status.PaidAt)
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
		t.Fatalf("got %d channels, want one virtual account and one QRIS rail (a redirect "+
			"wallet, a retail counter and the three QRIS aliases are not separate choices): %+v",
			len(channels), channels)
	}
	for _, channel := range channels {
		if channel.Code == "DANA" || channel.Code == "ALFAMIDI" {
			t.Fatalf("an unusable channel leaked into checkout: %+v", channel)
		}
	}
	if channels[0].Code != "BRIVA" || channels[0].FeeCustomer.FlatIDR != 4250 {
		t.Fatalf("first channel = %+v", channels[0])
	}
	if channels[1].Code != "QRIS" || channels[1].FeeCustomer.Percent != 0.7 {
		t.Fatalf("second channel = %+v, want the canonical QRIS", channels[1])
	}
	// The vendor groups QRIS under E-Wallet, so the group allowlist alone would
	// drop the only channel a Rp 5.000 Package can use.
	if channels[1].Group != "E-Wallet" {
		t.Fatalf("QRIS group = %q, want the vendor's E-Wallet", channels[1].Group)
	}
	// The amount window is what keeps a Rp 5.000 Package away from a virtual
	// account that only takes Rp 10.000 and up.
	if channels[0].MinAmountIDR != 10000 || channels[0].MaxAmountIDR != 10000000 {
		t.Fatalf("BRIVA window = %d..%d, want 10000..10000000",
			channels[0].MinAmountIDR, channels[0].MaxAmountIDR)
	}
	if channels[1].MinAmountIDR != 1000 || channels[1].MaxAmountIDR != 5000000 {
		t.Fatalf("QRIS window = %d..%d, want 1000..5000000",
			channels[1].MinAmountIDR, channels[1].MaxAmountIDR)
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
