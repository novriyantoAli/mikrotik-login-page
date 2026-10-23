package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/securetoken"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// outboxRow is the raw shape this test asserts on; the domain entity is read by
// the service, not by the test.
type outboxRow struct {
	ID          uint
	PhoneNumber string `gorm:"column:phone_number"`
	Message     string
	State       string
}

func (outboxRow) TableName() string { return "sms_outbox" }

func requestMagicLink(t *testing.T, harness *testutil.APIHarness, body map[string]any, clientIP string) *httptestResponse {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}

	recorder := harness.DoRequest(t, http.MethodPost, "/api/auth/magic-link", payload, func(r *http.Request) {
		r.RemoteAddr = clientIP + ":54321"
	})

	return &httptestResponse{
		code: recorder.Code,
		body: recorder.Body.Bytes(),
	}
}

type httptestResponse struct {
	code int
	body []byte
}

func (r *httptestResponse) decode(t *testing.T) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(r.body, &decoded); err != nil {
		t.Fatalf("decode %q: %v", r.body, err)
	}
	return decoded
}

// magicLinkTokenFrom reads the queued SMS for one Phone Number and pulls the
// token out of the link it carries — exactly what the buyer would tap.
func magicLinkTokenFrom(t *testing.T, harness *testutil.APIHarness, canonical string) string {
	t.Helper()

	var rows []outboxRow
	if err := harness.Postgres.
		Where("phone_number = ?", canonical).
		Order("id DESC").
		Limit(1).
		Find(&rows).Error; err != nil {
		t.Fatalf("read sms_outbox: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("no SMS queued for %s", canonical)
	}

	start := strings.Index(rows[0].Message, "http")
	if start == -1 {
		t.Fatalf("SMS %q carries no link", rows[0].Message)
	}
	end := strings.Index(rows[0].Message[start:], " ")
	if end == -1 {
		t.Fatalf("SMS %q has no text after the link", rows[0].Message)
	}
	link, err := url.Parse(rows[0].Message[start : start+end])
	if err != nil {
		t.Fatalf("parse Magic Link: %v", err)
	}
	if token := link.Query().Get("token"); token != "" {
		return token
	}
	t.Fatalf("Magic Link %q carries no token", link.String())
	return ""
}

func TestMagicLinkRequestAnswersIdenticallyForKnownAndUnknownNumbers(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	known := requestMagicLink(t, harness, map[string]any{"phone_number": "081200000001"}, "198.51.100.21")
	unknown := requestMagicLink(t, harness, map[string]any{"phone_number": "081200000002"}, "198.51.100.22")

	if known.code != http.StatusAccepted || unknown.code != http.StatusAccepted {
		t.Fatalf("status codes = %d and %d, want 202 for both", known.code, unknown.code)
	}
	if string(known.body) != string(unknown.body) {
		t.Fatalf("responses differ:\n known: %s\nunknown: %s", known.body, unknown.body)
	}

	decoded := known.decode(t)
	data, ok := decoded["data"].(map[string]any)
	if !ok || data["sent"] != true {
		t.Fatalf("response = %s, want data.sent = true", known.body)
	}
}

func TestMagicLinkRequestQueuesExactlyOneSmsWithAHashedToken(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	const canonical = "6281200000003"

	response := requestMagicLink(t, harness, map[string]any{"phone_number": "0812-0000-0003"}, "198.51.100.23")
	if response.code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", response.code, response.body)
	}

	var queue []outboxRow
	if err := harness.Postgres.Where("phone_number = ?", canonical).Find(&queue).Error; err != nil {
		t.Fatalf("read sms_outbox: %v", err)
	}
	if len(queue) != 1 {
		t.Fatalf("queued %d SMS, want 1", len(queue))
	}
	if queue[0].State != "pending" {
		t.Fatalf("row state = %q, want pending (the API never sends)", queue[0].State)
	}

	token := magicLinkTokenFrom(t, harness, canonical)

	var links []entity.MagicLinkToken
	if err := harness.Postgres.Where("phone_number = ?", canonical).Find(&links).Error; err != nil {
		t.Fatalf("read magic_link_tokens: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("stored %d tokens, want 1", len(links))
	}
	if links[0].TokenHash == token {
		t.Fatal("the raw token was stored; only its hash may be")
	}
	if links[0].TokenHash != securetoken.Hash(token) {
		t.Fatal("stored hash does not match the token in the SMS")
	}
	if links[0].UsedAt != nil {
		t.Fatal("a freshly issued token must be unused")
	}
}

func TestMagicLinkExchangeOpensASessionCookieThatCanBeRevoked(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	const canonical = "6281200000004"

	if response := requestMagicLink(t, harness,
		map[string]any{"phone_number": "081200000004"}, "198.51.100.24"); response.code != http.StatusAccepted {
		t.Fatalf("request status = %d (%s)", response.code, response.body)
	}
	token := magicLinkTokenFrom(t, harness, canonical)

	exchange := harness.Do(t, http.MethodPost, "/api/auth/magic-link/exchange",
		mustJSON(t, map[string]any{"token": token}))
	if exchange.Code != http.StatusOK {
		t.Fatalf("exchange status = %d (%s)", exchange.Code, exchange.Body.String())
	}

	cookies := exchange.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("exchange set %d cookies, want 1", len(cookies))
	}
	session := cookies[0]
	if session.Name != "session" || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v, want an HttpOnly SameSite=Lax session", session)
	}
	if session.Value == token {
		t.Fatal("the session cookie must not be the Magic Link token")
	}

	// The response body never carries the cookie value.
	if strings.Contains(exchange.Body.String(), session.Value) {
		t.Fatal("the session token leaked into the response body")
	}

	var stored []entity.Session
	if err := harness.Postgres.Where("phone_number = ?", canonical).Find(&stored).Error; err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	if len(stored) != 1 || stored[0].TokenHash != securetoken.Hash(session.Value) {
		t.Fatal("the session is not stored as the hash of the cookie value")
	}

	// Replaying the same link is refused and opens nothing new.
	replay := harness.Do(t, http.MethodPost, "/api/auth/magic-link/exchange",
		mustJSON(t, map[string]any{"token": token}))
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replayed exchange status = %d, want 400", replay.Code)
	}
	if code := errorCode(t, replay.Body.Bytes()); code != httpx.CodeMagicLinkInvalid {
		t.Fatalf("replay error code = %q, want %q", code, httpx.CodeMagicLinkInvalid)
	}
	if count := countSessions(t, harness, canonical); count != 1 {
		t.Fatalf("sessions after replay = %d, want 1", count)
	}

	// The session identifies exactly the Phone Number that bought the link.
	sessionRead := harness.DoRequest(t, http.MethodGet, "/api/auth/session", nil, func(r *http.Request) {
		r.AddCookie(session)
	})
	if sessionRead.Code != http.StatusOK {
		t.Fatalf("session status = %d (%s)", sessionRead.Code, sessionRead.Body.String())
	}
	var sessionBody struct {
		Data struct {
			PhoneNumber string `json:"phone_number"`
		} `json:"data"`
	}
	if err := json.Unmarshal(sessionRead.Body.Bytes(), &sessionBody); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if sessionBody.Data.PhoneNumber != canonical {
		t.Fatalf("session Phone Number = %q, want %q", sessionBody.Data.PhoneNumber, canonical)
	}

	// Sign-out revokes server-side: the same cookie stops working.
	signOut := harness.DoRequest(t, http.MethodPost, "/api/auth/logout", nil, func(r *http.Request) {
		r.AddCookie(session)
	})
	if signOut.Code != http.StatusNoContent {
		t.Fatalf("sign-out status = %d", signOut.Code)
	}

	afterSignOut := harness.DoRequest(t, http.MethodGet, "/api/auth/session", nil, func(r *http.Request) {
		r.AddCookie(session)
	})
	if afterSignOut.Code != http.StatusUnauthorized {
		t.Fatalf("session after sign-out = %d, want 401", afterSignOut.Code)
	}
}

func TestMagicLinkRequestWithoutASessionIsUnauthorized(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	response := harness.Do(t, http.MethodGet, "/api/auth/session", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if code := errorCode(t, response.Body.Bytes()); code != httpx.CodeUnauthorized {
		t.Fatalf("error code = %q, want %q", code, httpx.CodeUnauthorized)
	}
}

func TestMagicLinkRequestRejectsAnInvalidPhoneNumber(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	response := requestMagicLink(t, harness, map[string]any{"phone_number": "12345"}, "198.51.100.25")
	if response.code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", response.code, response.body)
	}
	if code := errorCode(t, response.body); code != httpx.CodeInvalidRequest {
		t.Fatalf("error code = %q, want %q", code, httpx.CodeInvalidRequest)
	}
}

func TestMagicLinkRequestReportsTheIpLimitWithAStableCode(t *testing.T) {
	t.Setenv("AUTH_RATE_LIMIT_IP_LIMIT", "2")

	harness := testutil.NewAPIHarness(t)
	const clientIP = "198.51.100.26"

	for i, phoneNumber := range []string{"081200000011", "081200000012"} {
		response := requestMagicLink(t, harness, map[string]any{"phone_number": phoneNumber}, clientIP)
		if response.code != http.StatusAccepted {
			t.Fatalf("request %d status = %d (%s)", i+1, response.code, response.body)
		}
	}

	limited := requestMagicLink(t, harness, map[string]any{"phone_number": "081200000013"}, clientIP)
	if limited.code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (%s)", limited.code, limited.body)
	}
	if code := errorCode(t, limited.body); code != httpx.CodeRateLimited {
		t.Fatalf("error code = %q, want %q", code, httpx.CodeRateLimited)
	}
}

func TestMagicLinkRequestSuppressesASecondSmsForTheSameNumber(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	const canonical = "6281200000005"

	first := requestMagicLink(t, harness, map[string]any{"phone_number": "081200000005"}, "198.51.100.27")
	second := requestMagicLink(t, harness, map[string]any{"phone_number": "081200000005"}, "198.51.100.28")

	if first.code != http.StatusAccepted || second.code != http.StatusAccepted {
		t.Fatalf("status codes = %d and %d, want 202 for both", first.code, second.code)
	}
	if string(first.body) != string(second.body) {
		t.Fatal("a suppressed request must answer exactly like the first one")
	}

	var queue []outboxRow
	if err := harness.Postgres.Where("phone_number = ?", canonical).Find(&queue).Error; err != nil {
		t.Fatalf("read sms_outbox: %v", err)
	}
	if len(queue) != 1 {
		t.Fatalf("queued %d SMS, want exactly 1 per window", len(queue))
	}
}

func TestMagicLinkDropsAReturnTargetOutsideTheAllowlist(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	const canonical = "6281200000006"

	response := requestMagicLink(t, harness, map[string]any{
		"phone_number": "081200000006",
		"return_url":   "https://evil.example/collect",
	}, "198.51.100.29")
	if response.code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", response.code, response.body)
	}

	var links []entity.MagicLinkToken
	if err := harness.Postgres.Where("phone_number = ?", canonical).Find(&links).Error; err != nil {
		t.Fatalf("read magic_link_tokens: %v", err)
	}
	if len(links) != 1 || links[0].ReturnURL != "" {
		t.Fatalf("return target %q survived; it must be dropped", links[0].ReturnURL)
	}
}

func mustJSON(t *testing.T, payload map[string]any) []byte {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	return encoded
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()

	var decoded struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return decoded.Error.Code
}

func countSessions(t *testing.T, harness *testutil.APIHarness, canonical string) int64 {
	t.Helper()

	var count int64
	if err := harness.Postgres.Model(&entity.Session{}).
		Where("phone_number = ?", canonical).
		Count(&count).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return count
}
