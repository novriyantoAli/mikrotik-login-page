package service_test

import (
	"context"
	"testing"

	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
)

// TestBackfillSnapshotsFillsOnlyPendingRows covers the deploy boundary in
// ADR 0026: pending Transactions created before the columns existed copy the
// catalog, while rows whose effect is already in FreeRADIUS keep their history.
func TestBackfillSnapshotsFillsOnlyPendingRows(t *testing.T) {
	t.Parallel()

	f := newFixture()
	ctx := context.Background()

	pending := f.addPaid(t)
	pending.State = entity.StatePending
	pending.DurationMinutes = 0
	pending.RateLimit = ""

	paid := f.addPaid(t)
	paid.State = entity.StatePaid
	paid.DurationMinutes = 0
	paid.RateLimit = ""

	provisioned := f.addPaid(t)
	provisioned.State = entity.StateProvisioned
	provisioned.DurationMinutes = 0
	provisioned.RateLimit = ""

	// The catalog is the snapshot source for rows that predate it, so a value that
	// differs from the current catalog is enough to prove where the row came from.
	f.catalog.packages[7] = packagesentity.Package{
		ID: 7, Name: "Paket Mingguan", PriceIDR: 25000,
		DurationMinutes: 5 * 24 * 60, RateLimit: "5M/5M",
	}

	filled, err := f.backfiller.BackfillSnapshots(ctx)
	if err != nil {
		t.Fatalf("BackfillSnapshots: %v", err)
	}
	if filled != 1 {
		t.Fatalf("backfilled %d rows, want only the pending one", filled)
	}

	if f.repo.rows[0].DurationMinutes != 5*24*60 || f.repo.rows[0].RateLimit != "5M/5M" {
		t.Fatalf("pending row snapshot = (%d, %q), want the catalog's (7200, 5M/5M)",
			f.repo.rows[0].DurationMinutes, f.repo.rows[0].RateLimit)
	}
	for _, index := range []int{1, 2} {
		if f.repo.rows[index].DurationMinutes != 0 || f.repo.rows[index].RateLimit != "" {
			t.Fatalf("row %d (state %q) was rewritten: (%d, %q)",
				index, f.repo.rows[index].State,
				f.repo.rows[index].DurationMinutes, f.repo.rows[index].RateLimit)
		}
	}

	// A second deploy runs the same command and must change nothing.
	filled, err = f.backfiller.BackfillSnapshots(ctx)
	if err != nil {
		t.Fatalf("second BackfillSnapshots: %v", err)
	}
	if filled != 0 {
		t.Fatalf("second run backfilled %d rows, want 0", filled)
	}
}

// TestBackfillSnapshotsSkipsAPendingRowWhosePackageIsGone keeps one orphan from
// blocking a deploy: the row is logged and left alone, never invented.
func TestBackfillSnapshotsSkipsAPendingRowWhosePackageIsGone(t *testing.T) {
	t.Parallel()

	f := newFixture()
	row := f.addPaid(t)
	row.State = entity.StatePending
	row.PackageID = 99
	row.DurationMinutes = 0
	row.RateLimit = ""

	filled, err := f.backfiller.BackfillSnapshots(context.Background())
	if err != nil {
		t.Fatalf("BackfillSnapshots: %v", err)
	}
	if filled != 0 {
		t.Fatalf("backfilled %d rows, want 0 for an unknown Package", filled)
	}
	if f.repo.rows[0].DurationMinutes != 0 || f.repo.rows[0].RateLimit != "" {
		t.Fatal("a row whose Package is gone was given invented terms")
	}
}
