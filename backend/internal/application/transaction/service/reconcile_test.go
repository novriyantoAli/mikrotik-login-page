package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
)

// settledStatus is what the gateway reports once the buyer has paid. The
// reconciliation sweep must turn it into the same `paid` state a callback does.
func settledStatus(reference string, amount int64, paidAt time.Time) payment.PaymentStatus {
	return payment.PaymentStatus{
		Reference: reference,
		Status:    payment.StatusPaid,
		Amount:    amount,
		PaidAt:    &paidAt,
	}
}

func TestReconcile_SettlesAPaymentTheCallbackNeverAnnounced(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())

	paidAt := f.clock.Now().Add(-2 * time.Minute)
	f.gateway.statuses[transaction.Reference] = settledStatus(transaction.Reference, transaction.AmountIDR, paidAt)

	reconciled, err := f.reconciler.ReconcileDue(context.Background())
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 1 {
		t.Fatalf("reconciled %d Transactions, want 1", reconciled)
	}

	stored, err := f.repo.GetByID(context.Background(), transaction.ID)
	if err != nil {
		t.Fatalf("read Transaction: %v", err)
	}
	if stored.State != entity.StatePaid {
		t.Fatalf("state = %q, want paid", stored.State)
	}
	if stored.PaidAt == nil || !stored.PaidAt.Equal(paidAt) {
		t.Fatalf("paid_at = %v, want the gateway's %s", stored.PaidAt, paidAt)
	}
	if len(f.waker.tasks) != 1 || f.waker.tasks[0] != service.TaskProvision {
		t.Fatalf("wake-up tasks = %v, want one %q", f.waker.tasks, service.TaskProvision)
	}

	// A settled Transaction leaves the selector, so the next sweep asks the
	// gateway nothing more about it.
	f.gateway.statusQueries = nil
	if reconciled, err := f.reconciler.ReconcileDue(context.Background()); err != nil || reconciled != 0 {
		t.Fatalf("second sweep = %d, %v; want 0, nil", reconciled, err)
	}
	if len(f.gateway.statusQueries) != 0 {
		t.Fatalf("second sweep asked the gateway about %v, want nothing", f.gateway.statusQueries)
	}
}

func TestReconcile_LeavesAnUnpaidTransactionAlone(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())

	reconciled, err := f.reconciler.ReconcileDue(context.Background())
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 0 {
		t.Fatalf("reconciled %d Transactions, want 0", reconciled)
	}
	if state := f.state(t, transaction.ID); state != entity.StatePending {
		t.Fatalf("state = %q, want pending", state)
	}
	if len(f.waker.tasks) != 0 {
		t.Fatalf("an unpaid Transaction woke provisioning: %v", f.waker.tasks)
	}
}

// TestReconcile_ReadFailureChangesNothing is the ADR 0018 rule: a status read is
// not authoritative, so an unreachable gateway must not settle, close or fail a
// Transaction.
func TestReconcile_ReadFailureChangesNothing(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())
	f.gateway.statusErr = errors.New("gateway unreachable")

	reconciled, err := f.reconciler.ReconcileDue(context.Background())
	if err != nil {
		t.Fatalf("ReconcileDue returned an error for one unreachable status read: %v", err)
	}
	if reconciled != 0 {
		t.Fatalf("reconciled %d Transactions, want 0", reconciled)
	}
	if state := f.state(t, transaction.ID); state != entity.StatePending {
		t.Fatalf("state = %q, want pending", state)
	}
}

// TestReconcile_MismatchedAmountLeavesTheRowPending proves the reconciliation
// path inherits the callback path's refusal instead of provisioning a purchase
// for the wrong amount.
func TestReconcile_MismatchedAmountLeavesTheRowPending(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())
	f.gateway.statuses[transaction.Reference] = settledStatus(transaction.Reference, transaction.AmountIDR-1000, f.clock.Now())

	reconciled, err := f.reconciler.ReconcileDue(context.Background())
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 0 {
		t.Fatalf("reconciled %d Transactions, want 0", reconciled)
	}
	if state := f.state(t, transaction.ID); state != entity.StatePending {
		t.Fatalf("state = %q, want pending for an operator to reconcile", state)
	}
	if len(f.waker.tasks) != 0 {
		t.Fatalf("a mismatched settlement woke provisioning: %v", f.waker.tasks)
	}
}

func TestReconcile_ClosesAPaymentTheGatewayReportsFailed(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())
	f.gateway.statuses[transaction.Reference] = payment.PaymentStatus{
		Reference: transaction.Reference,
		Status:    payment.StatusExpired,
		Amount:    transaction.AmountIDR,
	}

	reconciled, err := f.reconciler.ReconcileDue(context.Background())
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 1 {
		t.Fatalf("reconciled %d Transactions, want 1", reconciled)
	}
	if state := f.state(t, transaction.ID); state != entity.StateExpired {
		t.Fatalf("state = %q, want expired", state)
	}
}

// TestReconcile_WindowIsStillPayableRows is the decision itself: a row whose
// deadline passed is not asked about, because `expired` is final and a
// settlement after it would only be a warning line (ADR 0015, ADR 0018).
func TestReconcile_WindowIsStillPayableRows(t *testing.T) {
	t.Parallel()

	f := newFixture()
	transaction := f.checkout(t, validRequest())
	f.gateway.statuses[transaction.Reference] = settledStatus(transaction.Reference, transaction.AmountIDR, f.clock.Now())

	f.clock.Advance(2 * time.Hour)

	reconciled, err := f.reconciler.ReconcileDue(context.Background())
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 0 {
		t.Fatalf("reconciled %d Transactions past their deadline, want 0", reconciled)
	}
	if state := f.state(t, transaction.ID); state != entity.StatePending {
		t.Fatalf("state = %q, want pending until the expiry sweep closes it", state)
	}
	if len(f.gateway.statusQueries) != 0 {
		t.Fatalf("the gateway was asked about %v, want no calls past the deadline",
			f.gateway.statusQueries)
	}
}

// TestReconcile_BoundsOneSweep proves the batch knob is honored, so a backlog
// is chipped away instead of holding one gateway round trip per row at once.
func TestReconcile_BoundsOneSweep(t *testing.T) {
	t.Parallel()

	f := newFixture()
	for index := 0; index < 3; index++ {
		reference := "TRX-BATCH-" + string(rune('A'+index))
		row := &entity.Transaction{
			Reference:   reference,
			PhoneNumber: testPhone,
			PackageID:   uint(index + 1),
			PackageName: "Paket Mingguan",
			AmountIDR:   25000,
			Channel:     "BRIVA",
			State:       entity.StatePending,
			ExpiresAt:   f.clock.Now().Add(time.Hour),
		}
		if err := f.repo.Create(context.Background(), row); err != nil {
			t.Fatalf("seed %s: %v", reference, err)
		}
		f.gateway.statuses[reference] = settledStatus(reference, row.AmountIDR, f.clock.Now())
	}

	reconciler := service.NewReconciler(
		f.repo,
		f.gateway,
		f.waker,
		f.clock,
		service.ReconciliationSettings{SweepInterval: time.Minute, Batch: 2},
		zap.NewNop(),
	)

	reconciled, err := reconciler.ReconcileDue(context.Background())
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 2 {
		t.Fatalf("reconciled %d Transactions, want the batch size 2", reconciled)
	}
	if len(f.gateway.statusQueries) != 2 {
		t.Fatalf("asked the gateway about %d references, want 2", len(f.gateway.statusQueries))
	}
}

func (f *fixture) state(t *testing.T, id uint) entity.State {
	t.Helper()

	row, err := f.repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("read Transaction %d: %v", id, err)
	}
	return row.State
}
