package integration

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// activePeriodEnvelope is the contract as Go sees it. The Login Page normalises
// {"data":{"expiry":...}} and treats a null expiry as "no Active Period".
type activePeriodEnvelope struct {
	Data dto.ActivePeriodResponse `json:"data"`
}

// TestActivePeriodContract_FixtureMatchesDTO proves the DTO models exactly the
// shared fixture, field names included.
func TestActivePeriodContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "active-period.response.json")

	var envelope activePeriodEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into the Active Period DTO: %v", err)
	}
	if envelope.Data.Expiry == nil {
		t.Fatal("fixture must carry an expiry")
	}
	if _, err := time.Parse(time.RFC3339, *envelope.Data.Expiry); err != nil {
		t.Fatalf("fixture expiry %q is not ISO-8601: %v", *envelope.Data.Expiry, err)
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal Active Period DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

// TestActivePeriodContract_LiveResponseMatchesFixture checks the running API
// against the fixture's shape.
func TestActivePeriodContract_LiveResponseMatchesFixture(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	harness.Doubles.RadiusReader.SetExpiration("628123456789", time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))

	status, body := harness.GetJSON(t, "/api/active-period?phone=628123456789")
	if status != 200 {
		t.Fatalf("status = %d, body = %v", status, body)
	}

	fixture := decodeJSON(t, loadContract(t, "active-period.response.json")).(map[string]any)
	fixtureData := fixture["data"].(map[string]any)
	liveData, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data is not an object: %T", body["data"])
	}
	assertSameKeys(t, fixtureData, liveData, "$.data")
}
