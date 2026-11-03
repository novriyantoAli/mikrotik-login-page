package integration

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	transactionentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestProvisioningUsesTheSnapshotFromCheckoutWhenTheCatalogChanges is the
// acceptance path for ADR 0026: an owner edits the Package between payment and
// provisioning, and the account still receives the terms that were bought.
func TestProvisioningUsesTheSnapshotFromCheckoutWhenTheCatalogChanges(t *testing.T) {
	harness, cookie, created := paidCheckout(t, "081200000051", "198.51.100.51")
	ctx := context.Background()

	var bought packagesentity.Package
	if err := harness.Postgres.Where("id = ?", created.PackageID).First(&bought).Error; err != nil {
		t.Fatalf("read the bought Package: %v", err)
	}

	// The catalog is edited after the money arrived. The Transaction must not
	// follow it.
	if err := harness.Postgres.Model(&packagesentity.Package{}).
		Where("id = ?", created.PackageID).
		Updates(map[string]any{
			"duration_minutes": 5 * 24 * 60,
			"rate_limit":       "1M/1M",
		}).Error; err != nil {
		t.Fatalf("edit the catalog: %v", err)
	}

	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	accounts := harness.Doubles.Radius.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("writer applied %d accounts, want 1", len(accounts))
	}
	account := accounts[0]
	if account.RateLimit != bought.RateLimit {
		t.Fatalf("rate limit = %q, want the bought %q", account.RateLimit, bought.RateLimit)
	}

	response := getTransaction(t, harness, cookie, created.ID)
	if response.PaidAt == nil {
		t.Fatal("a provisioned Transaction must carry paid_at")
	}
	paidAt, err := time.Parse(time.RFC3339, *response.PaidAt)
	if err != nil {
		t.Fatalf("parse paid_at: %v", err)
	}
	wantExpiration := paidAt.Add(time.Duration(bought.DurationMinutes) * time.Minute)
	if !account.Expiration.Equal(wantExpiration) {
		t.Fatalf("expiration = %s, want the bought duration (%s)",
			account.Expiration, wantExpiration)
	}

	var stored transactionentity.Transaction
	if err := harness.Postgres.Where("id = ?", created.ID).First(&stored).Error; err != nil {
		t.Fatalf("read the Transaction: %v", err)
	}
	if stored.DurationMinutes != bought.DurationMinutes || stored.RateLimit != bought.RateLimit {
		t.Fatalf("stored snapshot = (%d, %q), want the bought terms (%d, %q)",
			stored.DurationMinutes, stored.RateLimit,
			bought.DurationMinutes, bought.RateLimit)
	}
}

// TestMigrateBackfillsUnprovisionedSnapshotsWithoutTouchingProvisionedRows proves
// the one-shot backfill on the real upgrade path: rows that predate the columns
// (simulated by dropping them) are filled from the catalog while pending or paid,
// and a provisioned row's history is left alone.
func TestMigrateBackfillsUnprovisionedSnapshotsWithoutTouchingProvisionedRows(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()
	repo := repository.NewRepository(harness.Postgres, zap.NewNop())

	packageID := firstPackageID(t, harness)
	var pkg packagesentity.Package
	if err := harness.Postgres.Where("id = ?", packageID).First(&pkg).Error; err != nil {
		t.Fatalf("read the Package: %v", err)
	}

	pending := newTransaction("TRX-SNAPSHOT-PENDING", "6281300000201", packageID,
		transactionentity.StatePending)
	if err := repo.Create(ctx, pending); err != nil {
		t.Fatalf("create the pending Transaction: %v", err)
	}
	paid := newTransaction("TRX-SNAPSHOT-PAID", "6281300000202", packageID,
		transactionentity.StatePaid)
	if err := repo.Create(ctx, paid); err != nil {
		t.Fatalf("create the paid Transaction: %v", err)
	}
	provisioned := newTransaction("TRX-SNAPSHOT-PROVISIONED", "6281300000203", packageID,
		transactionentity.StateProvisioned)
	if err := repo.Create(ctx, provisioned); err != nil {
		t.Fatalf("create the provisioned Transaction: %v", err)
	}

	// Rewind the schema to the deploy boundary this migration exists for: the
	// columns are gone and the rows above were written before they existed.
	if err := harness.Postgres.Exec(
		"ALTER TABLE transactions DROP COLUMN duration_minutes, DROP COLUMN rate_limit",
	).Error; err != nil {
		t.Fatalf("drop the snapshot columns: %v", err)
	}

	harness.Migrate(t)

	for name, id := range map[string]uint{"pending": pending.ID, "paid": paid.ID} {
		var filled transactionentity.Transaction
		if err := harness.Postgres.Where("id = ?", id).First(&filled).Error; err != nil {
			t.Fatalf("read the %s Transaction: %v", name, err)
		}
		if filled.DurationMinutes != pkg.DurationMinutes || filled.RateLimit != pkg.RateLimit {
			t.Fatalf("%s snapshot = (%d, %q), want the catalog's (%d, %q)",
				name, filled.DurationMinutes, filled.RateLimit, pkg.DurationMinutes, pkg.RateLimit)
		}
	}

	var untouched transactionentity.Transaction
	if err := harness.Postgres.Where("id = ?", provisioned.ID).First(&untouched).Error; err != nil {
		t.Fatalf("read the provisioned Transaction: %v", err)
	}
	if untouched.DurationMinutes != 0 || untouched.RateLimit != "" {
		t.Fatalf("a provisioned row's history was rewritten: (%d, %q)",
			untouched.DurationMinutes, untouched.RateLimit)
	}

	// A second deploy is a no-op.
	harness.Migrate(t)

	var again transactionentity.Transaction
	if err := harness.Postgres.Where("id = ?", pending.ID).First(&again).Error; err != nil {
		t.Fatalf("read the Transaction after the second migration: %v", err)
	}
	if again.DurationMinutes != pkg.DurationMinutes || again.RateLimit != pkg.RateLimit {
		t.Fatalf("second migration changed the snapshot: (%d, %q)",
			again.DurationMinutes, again.RateLimit)
	}
}

// TestMigrateBackfillPreservesAnAlreadyFrozenDuration pins the per-column fill:
// an empty rate limit is filled from the catalog without overwriting a duration
// that checkout already recorded.
func TestMigrateBackfillPreservesAnAlreadyFrozenDuration(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()
	repo := repository.NewRepository(harness.Postgres, zap.NewNop())

	packageID := firstPackageID(t, harness)
	var pkg packagesentity.Package
	if err := harness.Postgres.Where("id = ?", packageID).First(&pkg).Error; err != nil {
		t.Fatalf("read the Package: %v", err)
	}

	row := newTransaction("TRX-SNAPSHOT-PARTIAL", "6281300000204", packageID,
		transactionentity.StatePending)
	row.DurationMinutes = 60
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("create the partially snapshotted Transaction: %v", err)
	}

	harness.Migrate(t)

	var filled transactionentity.Transaction
	if err := harness.Postgres.Where("id = ?", row.ID).First(&filled).Error; err != nil {
		t.Fatalf("read the backfilled Transaction: %v", err)
	}
	if filled.DurationMinutes != 60 {
		t.Fatalf("duration = %d, want the already frozen 60", filled.DurationMinutes)
	}
	if filled.RateLimit != pkg.RateLimit {
		t.Fatalf("rate limit = %q, want the catalog's %q", filled.RateLimit, pkg.RateLimit)
	}
}
