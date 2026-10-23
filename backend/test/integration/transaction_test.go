package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// newSessionCookie walks the real Magic Link path and returns the session cookie
// a browser would then send. Nothing here mints a session directly: the guard is
// tested through the same door a buyer uses.
func newSessionCookie(t *testing.T, harness *testutil.APIHarness, inputPhone, clientIP string) *http.Cookie {
	t.Helper()

	if response := requestMagicLink(t, harness, map[string]any{"phone_number": inputPhone}, clientIP); response.code != http.StatusAccepted {
		t.Fatalf("magic link request status = %d (%s)", response.code, response.body)
	}
	token := magicLinkTokenFrom(t, harness, "62"+inputPhone[1:])

	exchange := harness.Do(t, http.MethodPost, "/api/auth/magic-link/exchange", mustJSON(t, map[string]any{"token": token}))
	if exchange.Code != http.StatusOK {
		t.Fatalf("exchange status = %d (%s)", exchange.Code, exchange.Body.String())
	}

	cookies := exchange.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("exchange set %d cookies, want 1", len(cookies))
	}
	return cookies[0]
}

func withCookie(cookie *http.Cookie, clientIP string) func(*http.Request) {
	return func(request *http.Request) {
		request.RemoteAddr = clientIP + ":54321"
		if cookie != nil {
			request.AddCookie(cookie)
		}
	}
}

func decodeTransaction(t *testing.T, recorder []byte) dto.TransactionResponse {
	t.Helper()

	var envelope struct {
		Data dto.TransactionResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder, &envelope); err != nil {
		t.Fatalf("decode transaction %q: %v", recorder, err)
	}
	return envelope.Data
}

func checkout(
	t *testing.T,
	harness *testutil.APIHarness,
	cookie *http.Cookie,
	packageID uint,
	channel string,
) dto.TransactionResponse {
	t.Helper()

	recorder := harness.DoRequest(t, http.MethodPost, "/api/transactions",
		mustJSON(t, map[string]any{"package_id": packageID, "channel": channel}),
		withCookie(cookie, "198.51.100.101"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /api/transactions = %d (%s)", recorder.Code, recorder.Body.String())
	}
	return decodeTransaction(t, recorder.Body.Bytes())
}

// postCallback delivers a settlement notification exactly as the gateway would:
// a JSON body plus a signature header.
func postCallback(
	t *testing.T,
	harness *testutil.APIHarness,
	reference string,
	status payment.Status,
	amount int64,
	signature string,
) int {
	t.Helper()

	recorder := harness.DoRequest(t, http.MethodPost, "/api/payments/callback",
		mustJSON(t, map[string]any{"reference": reference, "status": status, "amount": amount}),
		func(request *http.Request) {
			request.Header.Set("X-Callback-Signature", signature)
		})
	return recorder.Code
}

func getTransaction(t *testing.T, harness *testutil.APIHarness, cookie *http.Cookie, id uint) dto.TransactionResponse {
	t.Helper()

	path := "/api/transactions/" + uintString(id)
	recorder := harness.DoRequest(t, http.MethodGet, path, nil, withCookie(cookie, "198.51.100.102"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d (%s)", path, recorder.Code, recorder.Body.String())
	}
	return decodeTransaction(t, recorder.Body.Bytes())
}

func uintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}

// TestCheckoutRequiresASession is the middleware integration test: the same
// production router answers 401 without a cookie and 200 with one.
func TestCheckoutRequiresASession(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	packageID := firstPackageID(t, harness)

	for _, path := range []string{"/api/transactions/channels", "/api/transactions/1"} {
		recorder := harness.Do(t, http.MethodGet, path, nil)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s without a session = %d, want 401", path, recorder.Code)
		}
	}

	recorder := harness.Do(t, http.MethodPost, "/api/transactions",
		mustJSON(t, map[string]any{"package_id": packageID, "channel": "BRIVA"}))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/transactions without a session = %d, want 401", recorder.Code)
	}

	cookie := newSessionCookie(t, harness, "081300000011", "198.51.100.11")
	channels := harness.DoRequest(t, http.MethodGet, "/api/transactions/channels", nil,
		withCookie(cookie, "198.51.100.111"))
	if channels.Code != http.StatusOK {
		t.Fatalf("GET channels with a session = %d (%s)", channels.Code, channels.Body.String())
	}
}

func TestChannelsComeFromTheGatewayWithSeparateFees(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000012", "198.51.100.12")

	recorder := harness.DoRequest(t, http.MethodGet, "/api/transactions/channels", nil,
		withCookie(cookie, "198.51.100.112"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", recorder.Code, recorder.Body.String())
	}

	var envelope struct {
		Data []dto.ChannelResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode channels: %v", err)
	}
	if len(envelope.Data) != 2 {
		t.Fatalf("got %d channels, want the fake's 2", len(envelope.Data))
	}
	if envelope.Data[0].FeeFlat != 2500 || envelope.Data[1].FeeRatio != 0.7 {
		t.Fatalf("fees lost: %+v", envelope.Data)
	}
}

func TestCheckoutTwiceReturnsTheSameTransaction(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000013", "198.51.100.13")
	packageID := firstPackageID(t, harness)

	first := checkout(t, harness, cookie, packageID, "BRIVA")
	second := checkout(t, harness, cookie, packageID, "BRIVA")

	if first.ID != second.ID || first.Reference != second.Reference {
		t.Fatalf("second checkout created a different Transaction: %+v vs %+v", first, second)
	}

	var stored int64
	if err := harness.Postgres.Model(&entity.Transaction{}).
		Where("phone_number = ?", "6281300000013").Count(&stored).Error; err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if stored != 1 {
		t.Fatalf("stored %d Transactions, want 1", stored)
	}
	if requests := harness.Doubles.Gateway.CreateRequests; len(requests) != 1 {
		t.Fatalf("gateway created %d bills, want 1", len(requests))
	}
	if first.AmountIDR != 5000 {
		t.Fatalf("amount = %d, want the seeded Package price 5000", first.AmountIDR)
	}
	if first.State != string(entity.StatePending) || first.PayCode == "" {
		t.Fatalf("checkout did not return instructions: %+v", first)
	}
}

func TestTransactionOfAnotherPhoneNumberIsNotFound(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	owner := newSessionCookie(t, harness, "081300000014", "198.51.100.14")
	stranger := newSessionCookie(t, harness, "081300000015", "198.51.100.15")
	packageID := firstPackageID(t, harness)

	transaction := checkout(t, harness, owner, packageID, "BRIVA")

	recorder := harness.DoRequest(t, http.MethodGet, "/api/transactions/"+uintString(transaction.ID), nil,
		withCookie(stranger, "198.51.100.115"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("another buyer's Transaction answered %d, want 404", recorder.Code)
	}

	// Naming someone else's Phone Number in the body is also "not found".
	recorder = harness.DoRequest(t, http.MethodPost, "/api/transactions",
		mustJSON(t, map[string]any{
			"package_id": packageID, "channel": "BRIVA", "phone_number": "6281300000014",
		}),
		withCookie(stranger, "198.51.100.115"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("foreign Phone Number in the body answered %d, want 404", recorder.Code)
	}

	// The owner may name their own number in any accepted form: the shared
	// normalization decides, not a raw string comparison.
	_, catalog := harness.GetJSON(t, "/api/packages")
	second := catalog["data"].([]any)[1].(map[string]any)["id"].(float64)
	recorder = harness.DoRequest(t, http.MethodPost, "/api/transactions",
		mustJSON(t, map[string]any{
			"package_id": uint(second), "channel": "BRIVA", "phone_number": "0813-0000-0014",
		}),
		withCookie(owner, "198.51.100.114"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("the owner's own number in national form answered %d (%s), want 200",
			recorder.Code, recorder.Body.String())
	}
}

func TestCallbackSettlesOnceAndRepeatsChangeNothing(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000016", "198.51.100.16")
	packageID := firstPackageID(t, harness)
	transaction := checkout(t, harness, cookie, packageID, "BRIVA")

	if code := postCallback(t, harness, transaction.Reference, payment.StatusPaid, 5000,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("first callback = %d, want 200", code)
	}
	paid := getTransaction(t, harness, cookie, transaction.ID)
	if paid.State != string(entity.StatePaid) || paid.PaidAt == nil {
		t.Fatalf("state after callback = %+v, want paid with paid_at", paid)
	}

	if code := postCallback(t, harness, transaction.Reference, payment.StatusPaid, 5000,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("repeated callback = %d, want 200 (acknowledged, ignored)", code)
	}

	again := getTransaction(t, harness, cookie, transaction.ID)
	if again.State != string(entity.StatePaid) {
		t.Fatalf("state after a repeated callback = %q, want paid", again.State)
	}
	if again.PaidAt == nil || *again.PaidAt != *paid.PaidAt {
		t.Fatalf("a repeated callback moved paid_at from %v to %v", paid.PaidAt, again.PaidAt)
	}
}

func TestCallbackWithAWrongSignatureIsRejectedAndChangesNothing(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000017", "198.51.100.17")
	packageID := firstPackageID(t, harness)
	transaction := checkout(t, harness, cookie, packageID, "BRIVA")

	if code := postCallback(t, harness, transaction.Reference, payment.StatusPaid, 5000,
		"not-the-signature"); code != http.StatusUnauthorized {
		t.Fatalf("forged callback = %d, want 401", code)
	}
	if state := getTransaction(t, harness, cookie, transaction.ID).State; state != string(entity.StatePending) {
		t.Fatalf("state after a forged callback = %q, want pending", state)
	}
}

func TestCallbackWithAMismatchedAmountIsRefused(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000018", "198.51.100.18")
	packageID := firstPackageID(t, harness)
	transaction := checkout(t, harness, cookie, packageID, "BRIVA")

	if code := postCallback(t, harness, transaction.Reference, payment.StatusPaid, 1000,
		testutil.ValidCallbackSignature); code != http.StatusConflict {
		t.Fatalf("mismatched amount = %d, want 409", code)
	}
	if state := getTransaction(t, harness, cookie, transaction.ID).State; state != string(entity.StatePending) {
		t.Fatalf("state after a mismatched callback = %q, want pending", state)
	}
}

// TestForbiddenTransitionIsRefusedAtTheHTTPBoundary proves the state machine is
// enforced in the domain, not by the app: a late "expired" notification for a
// paid Transaction is acknowledged and ignored.
func TestForbiddenTransitionIsRefusedAtTheHTTPBoundary(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000019", "198.51.100.19")
	packageID := firstPackageID(t, harness)
	transaction := checkout(t, harness, cookie, packageID, "BRIVA")

	if code := postCallback(t, harness, transaction.Reference, payment.StatusPaid, 5000,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("settle = %d", code)
	}
	if code := postCallback(t, harness, transaction.Reference, payment.StatusExpired, 5000,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("late expiry = %d, want 200 (acknowledged)", code)
	}

	if state := getTransaction(t, harness, cookie, transaction.ID).State; state != string(entity.StatePaid) {
		t.Fatalf("paid → expired was allowed: state = %q", state)
	}
}

func TestPendingTransactionExpiresOnTheDeadline(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000020", "198.51.100.20")
	packageID := firstPackageID(t, harness)
	transaction := checkout(t, harness, cookie, packageID, "BRIVA")

	// The clock is the injected one, so the deadline is reached without waiting.
	harness.Doubles.Clock.Advance(2 * time.Hour)
	if state := getTransaction(t, harness, cookie, transaction.ID).State; state != string(entity.StatePending) {
		t.Fatalf("state before the sweep = %q, want pending", state)
	}

	expired, err := harness.TransactionExpiry.ExpireDue(context.Background())
	if err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired %d Transactions, want 1", expired)
	}
	if state := getTransaction(t, harness, cookie, transaction.ID).State; state != string(entity.StateExpired) {
		t.Fatalf("state after the sweep = %q, want expired", state)
	}

	// A late settlement cannot revive an expired Transaction.
	if code := postCallback(t, harness, transaction.Reference, payment.StatusPaid, 5000,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("late settle = %d, want 200 (acknowledged)", code)
	}
	if state := getTransaction(t, harness, cookie, transaction.ID).State; state != string(entity.StateExpired) {
		t.Fatalf("expired → paid was allowed: state = %q", state)
	}
}

// TestCheckoutAfterTheDeadlineOpensANewTransaction covers the window between a
// deadline passing and the worker sweep: the one-pending index must not turn a
// legitimate second purchase into a server error.
func TestCheckoutAfterTheDeadlineOpensANewTransaction(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000022", "198.51.100.22")
	packageID := firstPackageID(t, harness)

	first := checkout(t, harness, cookie, packageID, "BRIVA")

	harness.Doubles.Clock.Advance(2 * time.Hour)
	second := checkout(t, harness, cookie, packageID, "BRIVA")
	if second.ID == first.ID {
		t.Fatal("checkout after the deadline returned the overdue Transaction")
	}

	if state := getTransaction(t, harness, cookie, first.ID).State; state != string(entity.StateExpired) {
		t.Fatalf("old Transaction state = %q, want expired", state)
	}
	if state := getTransaction(t, harness, cookie, second.ID).State; state != string(entity.StatePending) {
		t.Fatalf("new Transaction state = %q, want pending", state)
	}
}

func TestCheckoutEchoesOnlyTheAllowlistedReturnTarget(t *testing.T) {
	t.Setenv("AUTH_LOGIN_PAGE_ALLOWLIST", "http://192.168.88.1/login")

	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000021", "198.51.100.21")
	packageID := firstPackageID(t, harness)

	allowed := harness.DoRequest(t, http.MethodPost, "/api/transactions",
		mustJSON(t, map[string]any{
			"package_id": packageID, "channel": "BRIVA",
			"return_url": "http://192.168.88.1/login?mac=AA:BB",
		}),
		withCookie(cookie, "198.51.100.121"))
	if allowed.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", allowed.Code, allowed.Body.String())
	}
	if got := decodeTransaction(t, allowed.Body.Bytes()).ReturnURL; got != "http://192.168.88.1/login?mac=AA:BB" {
		t.Fatalf("return_url = %q, want the allowlisted target", got)
	}

	// A second Package is needed because the first one is now pending.
	_, catalog := harness.GetJSON(t, "/api/packages")
	second := catalog["data"].([]any)[1].(map[string]any)["id"].(float64)

	refused := harness.DoRequest(t, http.MethodPost, "/api/transactions",
		mustJSON(t, map[string]any{
			"package_id": uint(second), "channel": "BRIVA",
			"return_url": "https://evil.example/collect",
		}),
		withCookie(cookie, "198.51.100.121"))
	if refused.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", refused.Code, refused.Body.String())
	}
	if got := decodeTransaction(t, refused.Body.Bytes()).ReturnURL; got != "" {
		t.Fatalf("return_url = %q, want it dropped by the allowlist", got)
	}
}
