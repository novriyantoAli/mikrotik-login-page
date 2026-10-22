package integration

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// packagesEnvelope is the contract as Go sees it. The shared fixture and the
// Login Page client seam mirror this: an extra or missing field is a breaking
// change and has to update the fixture, this DTO and the client test together.
type packagesEnvelope struct {
	Data []dto.PackageResponse `json:"data"`
}

// TestPackagesContract_FixtureMatchesDTO proves the DTO models exactly the
// shared fixture: unknown fields fail decoding and a forgotten field fails the
// round-trip comparison.
func TestPackagesContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "packages.response.json")

	var envelope packagesEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into the packages DTO: %v", err)
	}

	if len(envelope.Data) == 0 {
		t.Fatal("fixture must contain at least one Package")
	}
	for _, pkg := range envelope.Data {
		if pkg.ID == 0 || pkg.Name == "" || pkg.Price == "" || pkg.ActivePeriod == "" || pkg.SpeedLimit == "" {
			t.Fatalf("fixture Package %+v is incomplete", pkg)
		}
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal packages DTO: %v", err)
	}

	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

// TestPackagesContract_LiveResponseMatchesFixture checks the running API
// against the fixture's shape. Values are not asserted: the catalog is seeded
// data, the field names and kinds are the contract.
func TestPackagesContract_LiveResponseMatchesFixture(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	status, body := harness.GetJSON(t, "/api/packages")
	if status != 200 {
		t.Fatalf("status = %d, body = %v", status, body)
	}

	fixture := decodeJSON(t, loadContract(t, "packages.response.json")).(map[string]any)
	fixtureData := fixture["data"].([]any)
	fixturePackage := fixtureData[0].(map[string]any)

	liveData, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %T", body["data"])
	}
	if len(liveData) != len(service.StarterCatalog()) {
		t.Fatalf("live catalog has %d packages, want the %d seeded ones", len(liveData), len(service.StarterCatalog()))
	}
	for _, entry := range liveData {
		livePackage, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("catalog entry is not an object: %T", entry)
		}
		assertSameKeys(t, fixturePackage, livePackage, "$.data[]")
	}
}
