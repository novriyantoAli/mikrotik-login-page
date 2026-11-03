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

// TestMigrateBackfillsPendingSnapshotsWithoutTouchingProvisionedRows proves the
// one-shot backfill: rows that predate the columns are filled from the catalog
// while they are still pending, and a provisioned row's history is left alone.
func TestMigrateBackfillsPendingSnapshotsWithoutTouchingProvisionedRows(t *testing.T) {
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
	provisioned := newTransaction("TRX-SNAPSHOT-PROVISIONED", "6281300000202", packageID,
		transactionentity.StateProvisioned)
	if err := repo.Create(ctx, provisioned); err != nil {
		t.Fatalf("create the provisioned Transaction: %v", err)
	}

	harness.Migrate(t)

	var filled transactionentity.Transaction
	if err := harness.Postgres.Where("id = ?", pending.ID).First(&filled).Error; err != nil {
		t.Fatalf("read the backfilled Transaction: %v", err)
	}
	if filled.DurationMinutes != pkg.DurationMinutes || filled.RateLimit != pkg.RateLimit {
		t.Fatalf("pending snapshot = (%d, %q), want the catalog's (%d, %q)",
			filled.DurationMinutes, filled.RateLimit, pkg.DurationMinutes, pkg.RateLimit)
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
