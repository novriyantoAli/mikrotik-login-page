package integration

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

func newTransaction(reference, phoneNumber string, packageID uint, state entity.State) *entity.Transaction {
	return &entity.Transaction{
		Reference:   reference,
		PhoneNumber: phoneNumber,
		PackageID:   packageID,
		PackageName: "Paket Mingguan",
		AmountIDR:   25000,
		Channel:     "BRIVA",
		State:       state,
		ExpiresAt:   time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC),
	}
}

// TestTransactionRepositoryEnforcesOnePendingPerSession proves the idempotency
// guard is the database's, not the service's: two pending Transactions for the
// same Phone Number and Package cannot exist, while a settled one frees the slot.
func TestTransactionRepositoryEnforcesOnePendingPerSession(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, zap.NewNop())
	ctx := context.Background()

	var indexes []string
	if err := harness.Postgres.Raw(
		"SELECT indexname FROM pg_indexes WHERE tablename = 'transactions' AND indexname = 'idx_txn_pending'",
	).Scan(&indexes).Error; err != nil {
		t.Fatalf("read indexes: %v", err)
	}
	if len(indexes) != 1 {
		t.Fatal("the partial unique index on pending Transactions is missing; checkout idempotency is not enforced")
	}

	if err := repo.Create(ctx, newTransaction("TRX-A", "6281300000091", 1, entity.StatePending)); err != nil {
		t.Fatalf("first pending: %v", err)
	}
	if err := repo.Create(ctx, newTransaction("TRX-B", "6281300000091", 1, entity.StatePending)); err == nil {
		t.Fatal("a second pending Transaction for the same session and Package was accepted")
	}

	if err := repo.Create(ctx, newTransaction("TRX-C", "6281300000091", 1, entity.StatePaid)); err != nil {
		t.Fatalf("a paid Transaction must not be blocked by the pending guard: %v", err)
	}
}

func TestTransactionRepositoryTransitionIsGuarded(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, zap.NewNop())
	ctx := context.Background()

	row := newTransaction("TRX-D", "6281300000092", 2, entity.StatePending)
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("create: %v", err)
	}

	paidAt := time.Date(2026, time.October, 23, 8, 30, 0, 0, time.UTC)
	changed, err := repo.Transition(ctx, row.ID, entity.StatePending, entity.StatePaid, paidAt)
	if err != nil || !changed {
		t.Fatalf("pending → paid: changed=%v err=%v", changed, err)
	}

	// The same transition again loses the guard instead of moving twice.
	changed, err = repo.Transition(ctx, row.ID, entity.StatePending, entity.StatePaid, paidAt)
	if err != nil {
		t.Fatalf("repeat transition: %v", err)
	}
	if changed {
		t.Fatal("a repeated transition reported success")
	}

	// A forbidden transition is refused at the SQL guard too: paid is not pending.
	changed, err = repo.Transition(ctx, row.ID, entity.StatePending, entity.StateExpired, paidAt)
	if err != nil {
		t.Fatalf("forbidden transition: %v", err)
	}
	if changed {
		t.Fatal("paid → expired was applied by the repository")
	}

	stored, err := repo.GetByID(ctx, row.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.State != entity.StatePaid || stored.PaidAt == nil {
		t.Fatalf("stored row = %+v, want paid with paid_at", stored)
	}
}

func TestTransactionRepositoryExpiresOnlyOverduePendingRows(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, zap.NewNop())
	ctx := context.Background()

	overdue := newTransaction("TRX-E", "6281300000093", 3, entity.StatePending)
	overdue.ExpiresAt = time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)
	future := newTransaction("TRX-F", "6281300000093", 4, entity.StatePending)
	future.ExpiresAt = time.Date(2026, time.October, 23, 10, 0, 0, 0, time.UTC)

	for _, row := range []*entity.Transaction{overdue, future} {
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("create %s: %v", row.Reference, err)
		}
	}

	expired, err := repo.ExpireOverdue(ctx, time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ExpireOverdue: %v", err)
	}
	if len(expired) != 1 {
		t.Fatalf("expired %d rows, want 1", len(expired))
	}
	// The sweep names the row it changed, so the caller can log the transition
	// without reading it back (ADR 0018).
	if expired[0].Reference != "TRX-E" || expired[0].PhoneNumber == "" {
		t.Fatalf("expired row = %+v, want the overdue Transaction's identity", expired[0])
	}

	storedOverdue, _ := repo.GetByReference(ctx, "TRX-E")
	storedFuture, _ := repo.GetByReference(ctx, "TRX-F")
	if storedOverdue.State != entity.StateExpired {
		t.Fatalf("overdue state = %q, want expired", storedOverdue.State)
	}
	if storedFuture.State != entity.StatePending {
		t.Fatalf("future state = %q, want pending", storedFuture.State)
	}
}

// TestTransactionRepositoryLookups covers the two lookups the service depends on
// for ownership and callbacks.
func TestTransactionRepositoryLookups(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, zap.NewNop())
	ctx := context.Background()

	row := newTransaction("TRX-G", "6281300000094", 1, entity.StatePending)
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("create: %v", err)
	}

	byReference, err := repo.GetByReference(ctx, "TRX-G")
	if err != nil || byReference.ID != row.ID {
		t.Fatalf("GetByReference: row=%+v err=%v", byReference, err)
	}

	if _, err := repo.GetByReference(ctx, "TRX-MISSING"); err != repository.ErrNotFound {
		t.Fatalf("unknown reference error = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetByID(ctx, row.ID+1000); err != repository.ErrNotFound {
		t.Fatalf("unknown id error = %v, want ErrNotFound", err)
	}

	pending, err := repo.Pending(ctx, "6281300000094", 1, time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC))
	if err != nil || pending == nil {
		t.Fatalf("Pending: row=%+v err=%v", pending, err)
	}

	// A deadline in the past is not "live": the idempotency read ignores it.
	stale, err := repo.Pending(ctx, "6281300000094", 1, time.Date(2026, time.October, 24, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Pending after the deadline: %v", err)
	}
	if stale != nil {
		t.Fatalf("an overdue Transaction was returned as pending: %+v", stale)
	}
}

// TestTransactionRepositoryReconcilableIDsIsTheWindow is the ADR 0018 selector:
// only pending rows still payable, soonest deadline first, and never more than
// one batch.
func TestTransactionRepositoryReconcilableIDsIsTheWindow(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	repo := repository.NewRepository(harness.Postgres, zap.NewNop())
	ctx := context.Background()

	now := time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)

	// Later deadline, then the one the sweep must look at first.
	later := newTransaction("TRX-LATER", "6281300000095", 1, entity.StatePending)
	later.ExpiresAt = now.Add(30 * time.Minute)
	sooner := newTransaction("TRX-SOONER", "6281300000095", 2, entity.StatePending)
	sooner.ExpiresAt = now.Add(5 * time.Minute)
	// Not reconcilable: the deadline passed, and the other two are not pending.
	overdue := newTransaction("TRX-OVERDUE", "6281300000095", 3, entity.StatePending)
	overdue.ExpiresAt = now.Add(-time.Minute)
	paid := newTransaction("TRX-PAID", "6281300000095", 4, entity.StatePaid)
	paid.ExpiresAt = now.Add(10 * time.Minute)
	expired := newTransaction("TRX-EXPIRED", "6281300000095", 5, entity.StateExpired)
	expired.ExpiresAt = now.Add(10 * time.Minute)

	for _, row := range []*entity.Transaction{later, sooner, overdue, paid, expired} {
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("create %s: %v", row.Reference, err)
		}
	}

	ids, err := repo.ReconcilableIDs(ctx, now, 50)
	if err != nil {
		t.Fatalf("ReconcilableIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("reconcilable ids = %v, want the two payable pending rows", ids)
	}
	if ids[0] != sooner.ID || ids[1] != later.ID {
		t.Fatalf("reconcilable ids = %v, want soonest deadline first (%d, %d)",
			ids, sooner.ID, later.ID)
	}

	batch, err := repo.ReconcilableIDs(ctx, now, 1)
	if err != nil {
		t.Fatalf("ReconcilableIDs with limit: %v", err)
	}
	if len(batch) != 1 || batch[0] != sooner.ID {
		t.Fatalf("batched ids = %v, want only the soonest (%d)", batch, sooner.ID)
	}
}
