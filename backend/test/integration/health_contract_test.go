package integration

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// healthEnvelope is the contract as Go sees it: the shared envelope plus the
// health DTO. The shared fixture and the frontend zod schema mirror this.
type healthEnvelope struct {
	Data dto.Health `json:"data"`
}

// TestHealthContract_FixtureMatchesDTO proves the Go DTO models exactly the
// shared fixture: an unknown field fails decoding, and a field the DTO adds or
// forgets fails the round-trip comparison. The frontend parses the same fixture
// with a strict zod schema, so a contract change has to update both sides.
func TestHealthContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "health.response.json")

	var envelope healthEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into the health DTO: %v", err)
	}

	if envelope.Data.Status != dto.StatusOK {
		t.Fatalf("fixture status = %q, want %q", envelope.Data.Status, dto.StatusOK)
	}
	for name, check := range envelope.Data.Checks {
		if check.Status != dto.CheckUp {
			t.Fatalf("fixture check %q status = %q, want %q", name, check.Status, dto.CheckUp)
		}
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal health DTO: %v", err)
	}

	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

// TestHealthContract_DegradedFixtureMatchesDTO locks the 503 body the app must
// parse: a down check carries a failure category, not a driver message.
func TestHealthContract_DegradedFixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "health.degraded.response.json")

	var envelope healthEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("degraded fixture does not decode into the health DTO: %v", err)
	}

	if envelope.Data.Status != dto.StatusDegraded {
		t.Fatalf("fixture status = %q, want %q", envelope.Data.Status, dto.StatusDegraded)
	}

	down := envelope.Data.Checks[database.CheckFreeRADIUSMySQL]
	if down.Status != dto.CheckDown {
		t.Fatalf("freeradius_mysql status = %q, want %q", down.Status, dto.CheckDown)
	}
	switch down.Error {
	case dto.FailureTimeout, dto.FailureUnreachable, dto.FailureAuthFailed, dto.FailureUnavailable:
	default:
		t.Fatalf("fixture failure category = %q", down.Error)
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal degraded health DTO: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

// TestHealthContract_LiveResponseMatchesFixture checks the running API against
// the fixture's shape without asserting volatile values (latency, timestamps).
func TestHealthContract_LiveResponseMatchesFixture(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	_, body := harness.GetJSON(t, "/api/health")
	fixture := decodeJSON(t, loadContract(t, "health.response.json")).(map[string]any)

	assertSameKeys(t, fixture, body, "$")

	fixtureData := fixture["data"].(map[string]any)
	liveData := body["data"].(map[string]any)
	assertSameKeys(t, fixtureData, liveData, "$.data")

	fixtureChecks := fixtureData["checks"].(map[string]any)
	liveChecks := liveData["checks"].(map[string]any)
	assertSameKeys(t, fixtureChecks, liveChecks, "$.data.checks")

	// A down dependency adds an "error" field, so each live check may have one
	// extra key but must otherwise match the fixture's shape.
	allowedExtras := map[string]bool{"error": true}
	for name := range fixtureChecks {
		fixtureCheck := fixtureChecks[name].(map[string]any)
		liveCheck := liveChecks[name].(map[string]any)

		for key := range liveCheck {
			if allowedExtras[key] {
				continue
			}
			if _, ok := fixtureCheck[key]; !ok {
				t.Fatalf("check %q has unexpected field %q", name, key)
			}
		}
		for key := range fixtureCheck {
			if _, ok := liveCheck[key]; !ok {
				t.Fatalf("check %q is missing field %q", name, key)
			}
		}

		if _, isDown := liveCheck["error"]; isDown {
			assertFailureCategory(t, liveCheck)
		}
	}

	if _, ok := liveChecks[database.CheckFreeRADIUSMySQL]; !ok {
		t.Fatal("freeradius_mysql check must be reported")
	}
}
