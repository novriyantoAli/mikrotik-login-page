package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

func countPackages(t *testing.T, harness *testutil.APIHarness) int64 {
	t.Helper()

	var count int64
	if err := harness.Postgres.Model(&entity.Package{}).Count(&count).Error; err != nil {
		t.Fatalf("count packages: %v", err)
	}
	return count
}

// TestPackagesServesTheSeededCatalog is the acceptance criterion: the deployment
// seed is what the public endpoint answers, in seed order.
func TestPackagesServesTheSeededCatalog(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	status, body := harness.GetJSON(t, "/api/packages")
	if status != 200 {
		t.Fatalf("status = %d, body = %v", status, body)
	}

	entries := body["data"].([]any)
	want := service.StarterCatalog()
	if len(entries) != len(want) {
		t.Fatalf("catalog has %d entries, want %d", len(entries), len(want))
	}

	for index, entry := range entries {
		fields := entry.(map[string]any)
		if fields["nama"] != want[index].Name {
			t.Fatalf("entry %d nama = %v, want %q", index, fields["nama"], want[index].Name)
		}
		if fields["harga"] != service.FormatRupiah(want[index].PriceIDR) {
			t.Fatalf("entry %d harga = %v", index, fields["harga"])
		}
		if fields["durasiActivePeriod"] != service.FormatActivePeriod(want[index].DurationMinutes) {
			t.Fatalf("entry %d durasiActivePeriod = %v", index, fields["durasiActivePeriod"])
		}
		if fields["batasKecepatan"] != service.FormatSpeedLimit(want[index].RateLimit) {
			t.Fatalf("entry %d batasKecepatan = %v", index, fields["batasKecepatan"])
		}
		if id, ok := fields["id"].(float64); !ok || id <= 0 {
			t.Fatalf("entry %d id = %v, want a positive package identity", index, fields["id"])
		}
	}
}

// TestSeedIsIdempotent re-runs the deployment migration and asserts the catalog
// neither grows nor changes an existing row.
func TestSeedIsIdempotent(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	seeded := countPackages(t, harness)
	if seeded != int64(len(service.StarterCatalog())) {
		t.Fatalf("seeded %d packages, want %d", seeded, len(service.StarterCatalog()))
	}

	// An operator's edit (there is no Admin Panel yet) must survive a redeploy.
	if err := harness.Postgres.Model(&entity.Package{}).
		Where("code = ?", "harian").
		Update("price_idr", 7500).Error; err != nil {
		t.Fatalf("edit a package by hand: %v", err)
	}

	harness.Migrate(t)

	if got := countPackages(t, harness); got != seeded {
		t.Fatalf("re-running the seed left %d packages, want %d", got, seeded)
	}

	var harian entity.Package
	if err := harness.Postgres.Where("code = ?", "harian").First(&harian).Error; err != nil {
		t.Fatalf("read back the edited package: %v", err)
	}
	if harian.PriceIDR != 7500 {
		t.Fatalf("re-seeding overwrote the operator's price: %d", harian.PriceIDR)
	}
}

// TestSeedAddsOnlyMissingPackages proves the seed fills gaps without touching
// rows that are already there.
func TestSeedAddsOnlyMissingPackages(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	if err := harness.Postgres.Where("code = ?", "harian").Delete(&entity.Package{}).Error; err != nil {
		t.Fatalf("delete one package: %v", err)
	}
	before := countPackages(t, harness)

	harness.Migrate(t)

	if got := countPackages(t, harness); got != before+1 {
		t.Fatalf("seed inserted %d packages, want 1", got-before)
	}
}

// TestPackagesEmptyCatalogIsAnEmptyArray covers the "no Package yet" state the
// Login Page renders as an empty list.
func TestPackagesEmptyCatalogIsAnEmptyArray(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	if err := harness.Postgres.Where("1 = 1").Delete(&entity.Package{}).Error; err != nil {
		t.Fatalf("empty the catalog: %v", err)
	}

	status, body := harness.GetJSON(t, "/api/packages")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	entries, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %T", body["data"])
	}
	if len(entries) != 0 {
		t.Fatalf("catalog has %d entries, want 0", len(entries))
	}
}

// TestPackagesDTOShapeKeepsTheFrozenFieldNames guards the exact keys the Login
// Page seam reads: renaming one is a breaking change, not a refactor.
func TestPackagesDTOShapeKeepsTheFrozenFieldNames(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(dto.PackageResponse{ID: 1, Name: "n", Price: "p", ActivePeriod: "a", SpeedLimit: "s"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, key := range []string{`"id"`, `"nama"`, `"harga"`, `"durasiActivePeriod"`, `"batasKecepatan"`} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("packages response is missing %s: %s", key, encoded)
		}
	}
}
