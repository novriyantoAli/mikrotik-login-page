package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// The auth fixtures are shared with the purchase app's zod schemas. These tests
// prove both sides model them the same way, and that the running API answers
// with the same shape.

type magicLinkSentEnvelope struct {
	Data dto.MagicLinkSentResponse `json:"data"`
}

type sessionEnvelope struct {
	Data dto.SessionResponse `json:"data"`
}

type exchangeEnvelope struct {
	Data dto.ExchangeMagicLinkResponse `json:"data"`
}

func decodeStrict(t *testing.T, raw []byte, target any) {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("fixture does not decode into its DTO: %v", err)
	}
}

func TestAuthContract_FixturesMatchDTOs(t *testing.T) {
	var request dto.RequestMagicLinkRequest
	decodeStrict(t, loadContract(t, "magic-link.request.json"), &request)
	if request.PhoneNumber == "" || request.PackageID == nil || request.ReturnURL == "" {
		t.Fatalf("request fixture is incomplete: %+v", request)
	}
	roundTripRequest, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, loadContract(t, "magic-link.request.json")),
		decodeJSON(t, roundTripRequest), "$")

	var sent magicLinkSentEnvelope
	decodeStrict(t, loadContract(t, "magic-link.response.json"), &sent)
	if !sent.Data.Sent {
		t.Fatal("the uniform answer fixture must say sent")
	}
	roundTripSent, err := json.Marshal(sent)
	if err != nil {
		t.Fatalf("marshal sent DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, loadContract(t, "magic-link.response.json")),
		decodeJSON(t, roundTripSent), "$")

	var session sessionEnvelope
	decodeStrict(t, loadContract(t, "session.response.json"), &session)
	if session.Data.PhoneNumber == "" {
		t.Fatal("session fixture carries no Phone Number")
	}
	if _, err := time.Parse(time.RFC3339, session.Data.ExpiresAt); err != nil {
		t.Fatalf("session expiry %q is not ISO-8601: %v", session.Data.ExpiresAt, err)
	}
	roundTripSession, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, loadContract(t, "session.response.json")),
		decodeJSON(t, roundTripSession), "$")

	var exchange exchangeEnvelope
	decodeStrict(t, loadContract(t, "magic-link.exchange.response.json"), &exchange)
	if exchange.Data.Purpose != "checkout" || exchange.Data.PackageID == nil {
		t.Fatalf("exchange fixture lost its intent: %+v", exchange.Data)
	}
	// The raw session token must never be part of the body contract.
	if bytes.Contains(loadContract(t, "magic-link.exchange.response.json"), []byte(`"token"`)) {
		t.Fatal("the exchange fixture carries a token field; the session token belongs in the cookie only")
	}
	roundTripExchange, err := json.Marshal(exchange)
	if err != nil {
		t.Fatalf("marshal exchange DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, loadContract(t, "magic-link.exchange.response.json")),
		decodeJSON(t, roundTripExchange), "$")
}

func TestAuthContract_LiveResponsesMatchFixtures(t *testing.T) {
	const clientIP = "198.51.100.40"
	const allowedReturn = "http://192.168.88.1/login?mac=aa-bb-cc-dd-ee-ff"

	t.Setenv("AUTH_LOGIN_PAGE_ALLOWLIST", allowedReturn)
	harness := testutil.NewAPIHarness(t)

	packageID := firstPackageID(t, harness)

	request := requestMagicLink(t, harness, map[string]any{
		"phone_number": "081234567890",
		"package_id":   packageID,
		"return_url":   allowedReturn,
	}, clientIP)
	if request.code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", request.code, request.body)
	}

	assertSameKeys(t,
		decodeJSON(t, loadContract(t, "magic-link.response.json")).(map[string]any)["data"].(map[string]any),
		request.decode(t)["data"].(map[string]any),
		"$.data",
	)

	token := magicLinkTokenFrom(t, harness, "6281234567890")

	exchange := harness.Do(t, http.MethodPost, "/api/auth/magic-link/exchange",
		mustJSON(t, map[string]any{"token": token}))
	if exchange.Code != http.StatusOK {
		t.Fatalf("exchange status = %d (%s)", exchange.Code, exchange.Body.String())
	}

	var liveExchange map[string]any
	if err := json.Unmarshal(exchange.Body.Bytes(), &liveExchange); err != nil {
		t.Fatalf("decode exchange: %v", err)
	}
	fixtureExchange := decodeJSON(t, loadContract(t, "magic-link.exchange.response.json")).(map[string]any)
	assertSameKeys(t,
		fixtureExchange["data"].(map[string]any),
		liveExchange["data"].(map[string]any),
		"$.data",
	)

	cookies := exchange.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("exchange set %d cookies, want 1", len(cookies))
	}

	sessionRead := harness.DoRequest(t, http.MethodGet, "/api/auth/session", nil, func(r *http.Request) {
		r.AddCookie(cookies[0])
	})
	if sessionRead.Code != http.StatusOK {
		t.Fatalf("session status = %d (%s)", sessionRead.Code, sessionRead.Body.String())
	}

	var liveSession map[string]any
	if err := json.Unmarshal(sessionRead.Body.Bytes(), &liveSession); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	fixtureSession := decodeJSON(t, loadContract(t, "session.response.json")).(map[string]any)
	assertSameKeys(t,
		fixtureSession["data"].(map[string]any),
		liveSession["data"].(map[string]any),
		"$.data",
	)
}

func firstPackageID(t *testing.T, harness *testutil.APIHarness) uint {
	t.Helper()

	var rows []struct {
		ID uint
	}
	if err := harness.Postgres.Raw("SELECT id FROM packages ORDER BY id LIMIT 1").Scan(&rows).Error; err != nil {
		t.Fatalf("read package catalog: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the seeded catalog is empty")
	}
	return rows[0].ID
}
