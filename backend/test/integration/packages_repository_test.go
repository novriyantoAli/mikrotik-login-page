package integration

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestPackageRepositoryListsAndSeeds covers the persistence edge directly: the
// HTTP tests prove the endpoint, this proves the two repository behaviors the
// seed depends on (seed order and conflict-does-nothing) against real Postgres.
func TestPackageRepositoryListsAndSeeds(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, zap.NewNop())
	ctx := context.Background()

	catalog, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatal("List returned an empty catalog; the seed should have run")
	}
	for index := 1; index < len(catalog); index++ {
		if catalog[index-1].ID >= catalog[index].ID {
			t.Fatalf("List is not in seed order: %d before %d", catalog[index-1].ID, catalog[index].ID)
		}
	}

	novel := entity.Package{
		Code:            "uji-repo",
		Name:            "Paket Uji",
		PriceIDR:        1000,
		DurationMinutes: 60,
		RateLimit:       "1M/1M",
	}
	inserted, err := repo.InsertMissing(ctx, []entity.Package{novel})
	if err != nil {
		t.Fatalf("InsertMissing: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("first InsertMissing inserted %d rows, want 1", inserted)
	}

	// A second run with different values must neither duplicate nor overwrite.
	novel.PriceIDR = 9999
	inserted, err = repo.InsertMissing(ctx, []entity.Package{novel})
	if err != nil {
		t.Fatalf("InsertMissing again: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("re-run inserted %d rows, want 0", inserted)
	}

	var stored entity.Package
	if err := harness.Postgres.Where("code = ?", "uji-repo").First(&stored).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.PriceIDR != 1000 {
		t.Fatalf("stored price = %d, want the originally seeded 1000", stored.PriceIDR)
	}

	if err := harness.Postgres.Where("code = ?", "uji-repo").Delete(&entity.Package{}).Error; err != nil {
		t.Fatalf("clean up: %v", err)
	}
}
