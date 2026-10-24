package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/dto"
	transactiondto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

type accountContractEnvelope struct {
	Data dto.AccountResponse `json:"data"`
}

type extendPreviewEnvelope struct {
	Data dto.ExtendPreviewResponse `json:"data"`
}

type historyContractEnvelope struct {
	Data       []transactiondto.TransactionSummaryResponse `json:"data"`
	TotalCount int64                                       `json:"total_count"`
	Page       int                                         `json:"page"`
	PageSize   int                                         `json:"page_size"`
}

// TestAccountContract_FixturesMatchDTOs proves the Account Status DTOs model
// exactly the shared fixtures: an unknown field fails decoding and a forgotten
// field fails the round-trip shape comparison. The app parses the same files with
// strict zod schemas (ADR 0017).
func TestAccountContract_FixturesMatchDTOs(t *testing.T) {
	raw := loadContract(t, "account.response.json")

	var envelope accountContractEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into dto.AccountResponse: %v", err)
	}
	if envelope.Data.PIN == nil || envelope.Data.Package == nil ||
		envelope.Data.ActivePeriod.ExpiresAt == nil {
		t.Fatalf("fixture is incomplete: %+v", envelope.Data)
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal account DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")

	previewRaw := loadContract(t, "account.extend-preview.response.json")
	var preview extendPreviewEnvelope
	previewDecoder := json.NewDecoder(bytes.NewReader(previewRaw))
	previewDecoder.DisallowUnknownFields()
	if err := previewDecoder.Decode(&preview); err != nil {
		t.Fatalf("fixture does not decode into dto.ExtendPreviewResponse: %v", err)
	}
	previewRoundTrip, err := json.Marshal(preview)
	if err != nil {
		t.Fatalf("marshal extend preview DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, previewRaw), decodeJSON(t, previewRoundTrip), "$")

	historyRaw := loadContract(t, "transactions.response.json")
	var history historyContractEnvelope
	historyDecoder := json.NewDecoder(bytes.NewReader(historyRaw))
	historyDecoder.DisallowUnknownFields()
	if err := historyDecoder.Decode(&history); err != nil {
		t.Fatalf("fixture does not decode into the history DTO: %v", err)
	}
	if len(history.Data) == 0 || history.TotalCount == 0 {
		t.Fatalf("fixture is incomplete: %+v", history)
	}
	historyRoundTrip, err := json.Marshal(history)
	if err != nil {
		t.Fatalf("marshal history DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, historyRaw), decodeJSON(t, historyRoundTrip), "$")
}

// TestAccountContract_LiveResponsesMatchFixtures walks a real provisioning and
// compares the Account Status reads with the shared fixtures, so a field the app
// does not know about (or a fixture the DTO forgets) fails here.
func TestAccountContract_LiveResponsesMatchFixtures(t *testing.T) {
	harness, cookie, created := paidCheckout(t, "081300000201", "198.51.100.201")
	if err := harness.TransactionProvisioning.Provision(context.Background(), created.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	harness.Doubles.RadiusReader.SetExpiration("6281300000201",
		harness.Doubles.Radius.Accounts()[0].Expiration)

	summary := harness.DoRequest(t, http.MethodGet, "/api/account", nil,
		withCookie(cookie, "198.51.100.211"))
	if summary.Code != http.StatusOK {
		t.Fatalf("GET /api/account = %d (%s)", summary.Code, summary.Body.String())
	}
	accountFixture := decodeJSON(t, loadContract(t, "account.response.json"))
	assertSameJSONShape(t, accountFixture, decodeJSON(t, summary.Body.Bytes()), "$")

	preview := harness.DoRequest(t, http.MethodGet,
		"/api/account/extend-preview?package_id="+uintString(firstPackageID(t, harness)), nil,
		withCookie(cookie, "198.51.100.212"))
	if preview.Code != http.StatusOK {
		t.Fatalf("GET extend-preview = %d (%s)", preview.Code, preview.Body.String())
	}
	previewFixture := decodeJSON(t, loadContract(t, "account.extend-preview.response.json"))
	assertSameJSONShape(t, previewFixture, decodeJSON(t, preview.Body.Bytes()), "$")

	history := harness.DoRequest(t, http.MethodGet, "/api/transactions?page=1&page_size=20", nil,
		withCookie(cookie, "198.51.100.213"))
	if history.Code != http.StatusOK {
		t.Fatalf("GET /api/transactions = %d (%s)", history.Code, history.Body.String())
	}
	historyFixture := decodeJSON(t, loadContract(t, "transactions.response.json"))
	liveHistory := decodeJSON(t, history.Body.Bytes())
	assertSameJSONShape(t, historyFixture, liveHistory, "$")

	// The list is the one Transaction shape that must never gain a PIN field.
	fixtureEntry := historyFixture.(map[string]any)["data"].([]any)[0].(map[string]any)
	if _, ok := fixtureEntry["pin"]; ok {
		t.Fatal("transactions.response.json carries a pin field")
	}
	for _, entry := range liveHistory.(map[string]any)["data"].([]any) {
		assertSameKeys(t, fixtureEntry, entry.(map[string]any), "$.data[]")
	}
}

// TestAccountContract_EmptySummaryShape pins the nulls of the empty state: the app
// needs package and pin to be present-and-null, not absent, so its schema can be
// strict (ADR 0017).
func TestAccountContract_EmptySummaryShape(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000202", "198.51.100.202")

	recorder := harness.DoRequest(t, http.MethodGet, "/api/account", nil,
		withCookie(cookie, "198.51.100.221"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/account = %d (%s)", recorder.Code, recorder.Body.String())
	}

	body := decodeJSON(t, recorder.Body.Bytes()).(map[string]any)
	data := body["data"].(map[string]any)
	assertSameKeys(t, map[string]any{
		"phone_number":  "6281300000202",
		"active_period": map[string]any{"expires_at": nil, "found": false, "state": "none"},
		"package":       nil,
		"pin":           nil,
	}, data, "$.data")
}
