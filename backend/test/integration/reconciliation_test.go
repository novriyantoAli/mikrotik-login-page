package integration

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	transactionentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestReconciliation_SettlesALostCallbackAndProvisionsOnce is #17's acceptance
// path: the gateway was paid, the callback never arrived, and the sweep alone
// takes the Transaction to `paid` and then to `provisioned` — with one FreeRADIUS
// account and one queued PIN, no matter how often either sweep runs (ADR 0018).
func TestReconciliation_SettlesALostCallbackAndProvisionsOnce(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()
	cookie := newSessionCookie(t, harness, "081300000051", "198.51.100.51")
	created := checkout(t, harness, cookie, firstPackageID(t, harness), "BRIVA")

	if state := getTransaction(t, harness, cookie, created.ID).State; state != string(transactionentity.StatePending) {
		t.Fatalf("state before the sweep = %q, want pending", state)
	}

	// The vendor knows about the payment; this system never heard about it.
	paidAt := harness.Doubles.Clock.Now().Add(-time.Minute)
	harness.Doubles.Gateway.Statuses[created.Reference] = payment.PaymentStatus{
		Reference: created.Reference,
		Status:    payment.StatusPaid,
		Amount:    created.AmountIDR,
		PaidAt:    &paidAt,
	}

	reconciled, err := harness.TransactionReconciliation.ReconcileDue(ctx)
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 1 {
		t.Fatalf("reconciled %d Transactions, want 1", reconciled)
	}

	paid := getTransaction(t, harness, cookie, created.ID)
	if paid.State != string(transactionentity.StatePaid) {
		t.Fatalf("state after reconciliation = %q, want paid", paid.State)
	}
	if paid.PaidAt == nil {
		t.Fatal("reconciled Transaction has no paid_at")
	}
	storedPaidAt, err := time.Parse(time.RFC3339, *paid.PaidAt)
	if err != nil {
		t.Fatalf("parse paid_at: %v", err)
	}
	if !storedPaidAt.Equal(paidAt) {
		t.Fatalf("paid_at = %s, want the gateway's %s", storedPaidAt, paidAt)
	}

	// Reconciliation settles; it does not provision. That stays the worker's job.
	if accounts := harness.Doubles.Radius.Accounts(); len(accounts) != 0 {
		t.Fatalf("reconciliation applied %d FreeRADIUS accounts, want 0", len(accounts))
	}

	if _, err := harness.TransactionProvisioning.ProvisionDue(ctx); err != nil {
		t.Fatalf("ProvisionDue: %v", err)
	}

	provisioned := getTransaction(t, harness, cookie, created.ID)
	if provisioned.State != string(transactionentity.StateProvisioned) {
		t.Fatalf("state after provisioning = %q, want provisioned", provisioned.State)
	}
	if provisioned.PIN == nil {
		t.Fatal("a provisioned Transaction read back through the API must carry its PIN")
	}

	accounts := harness.Doubles.Radius.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("FreeRADIUS holds %d accounts after one reconciled purchase, want 1", len(accounts))
	}
	if rows := smsRows(t, harness, provisioned.PhoneNumber); len(rows) != 1 {
		t.Fatalf("outbox holds %d PIN messages, want 1", len(rows))
	}

	pkg, err := harness.Catalog.GetPackage(ctx, provisioned.PackageID)
	if err != nil {
		t.Fatalf("read Package: %v", err)
	}
	wantExpiration := paidAt.Add(time.Duration(pkg.DurationMinutes) * time.Minute)
	if !accounts[0].Expiration.Equal(wantExpiration) {
		t.Fatalf("expiration = %s, want settlement + duration (%s)",
			accounts[0].Expiration, wantExpiration)
	}

	// Replaying both sweeps must change nothing: the guarded transitions and the
	// selector keep a settlement from being applied twice (ADR 0015, ADR 0018).
	before := harness.Doubles.Gateway.StatusQueries()
	reconciled, err = harness.TransactionReconciliation.ReconcileDue(ctx)
	if err != nil || reconciled != 0 {
		t.Fatalf("replayed reconciliation = %d, %v; want 0, nil", reconciled, err)
	}
	if after := harness.Doubles.Gateway.StatusQueries(); len(after) != len(before) {
		t.Fatalf("the replayed sweep asked the gateway about %v, want no new calls", after)
	}
	if done, err := harness.TransactionProvisioning.ProvisionDue(ctx); err != nil || done != 0 {
		t.Fatalf("replayed provisioning = %d, %v; want 0, nil", done, err)
	}

	if accounts := harness.Doubles.Radius.Accounts(); len(accounts) != 1 {
		t.Fatalf("FreeRADIUS holds %d accounts after replaying the sweeps, want 1", len(accounts))
	}
	again := getTransaction(t, harness, cookie, created.ID)
	if again.PIN == nil || *again.PIN != *provisioned.PIN {
		t.Fatalf("replaying the sweeps rotated the PIN: %v → %v", provisioned.PIN, again.PIN)
	}
	if rows := smsRows(t, harness, provisioned.PhoneNumber); len(rows) != 1 {
		t.Fatalf("replaying the sweeps queued another PIN message: %d rows", len(rows))
	}
}

// TestReconciliation_DoesNotTouchAPaymentPastItsDeadline pins the window from
// ADR 0018: `expired` is final, so a Transaction whose deadline passed is never
// asked about — the expiry sweep closes it instead.
func TestReconciliation_DoesNotTouchAPaymentPastItsDeadline(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()
	cookie := newSessionCookie(t, harness, "081300000052", "198.51.100.52")
	created := checkout(t, harness, cookie, firstPackageID(t, harness), "BRIVA")

	paidAt := harness.Doubles.Clock.Now()
	harness.Doubles.Gateway.Statuses[created.Reference] = payment.PaymentStatus{
		Reference: created.Reference,
		Status:    payment.StatusPaid,
		Amount:    created.AmountIDR,
		PaidAt:    &paidAt,
	}
	harness.Doubles.Clock.Advance(2 * time.Hour)

	reconciled, err := harness.TransactionReconciliation.ReconcileDue(ctx)
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 0 {
		t.Fatalf("reconciled %d Transactions past their deadline, want 0", reconciled)
	}
	if queries := harness.Doubles.Gateway.StatusQueries(); len(queries) != 0 {
		t.Fatalf("the gateway was asked about %v, want no calls past the deadline", queries)
	}
	if state := getTransaction(t, harness, cookie, created.ID).State; state != string(transactionentity.StatePending) {
		t.Fatalf("state = %q, want pending until the expiry sweep closes it", state)
	}

	if expired, err := harness.TransactionExpiry.ExpireDue(ctx); err != nil || expired != 1 {
		t.Fatalf("ExpireDue = %d, %v; want 1, nil", expired, err)
	}
	if state := getTransaction(t, harness, cookie, created.ID).State; state != string(transactionentity.StateExpired) {
		t.Fatalf("state after the expiry sweep = %q, want expired", state)
	}
}

// TestReconciliation_KeepsPendingWhenTheGatewayIsUnreachable proves a status read
// that fails is not treated as an answer: the callback path stays open and the
// deadline keeps running.
func TestReconciliation_KeepsPendingWhenTheGatewayIsUnreachable(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()
	cookie := newSessionCookie(t, harness, "081300000053", "198.51.100.53")
	created := checkout(t, harness, cookie, firstPackageID(t, harness), "BRIVA")

	harness.Doubles.Gateway.StatusErr = errors.New("vendor unreachable")

	reconciled, err := harness.TransactionReconciliation.ReconcileDue(ctx)
	if err != nil {
		t.Fatalf("ReconcileDue should survive one unreachable status read: %v", err)
	}
	if reconciled != 0 {
		t.Fatalf("reconciled %d Transactions, want 0", reconciled)
	}
	if state := getTransaction(t, harness, cookie, created.ID).State; state != string(transactionentity.StatePending) {
		t.Fatalf("state = %q, want pending", state)
	}
}

// TestReconciliation_RefusesAMismatchedAmount keeps the callback path's rule on
// the sweep: money that arrived for a different amount needs an operator, not a
// provisioned account.
func TestReconciliation_RefusesAMismatchedAmount(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()
	cookie := newSessionCookie(t, harness, "081300000054", "198.51.100.54")
	created := checkout(t, harness, cookie, firstPackageID(t, harness), "BRIVA")

	paidAt := harness.Doubles.Clock.Now()
	harness.Doubles.Gateway.Statuses[created.Reference] = payment.PaymentStatus{
		Reference: created.Reference,
		Status:    payment.StatusPaid,
		Amount:    created.AmountIDR - 1000,
		PaidAt:    &paidAt,
	}

	reconciled, err := harness.TransactionReconciliation.ReconcileDue(ctx)
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 0 {
		t.Fatalf("reconciled %d Transactions, want 0", reconciled)
	}
	response := getTransaction(t, harness, cookie, created.ID)
	if response.State != string(transactionentity.StatePending) {
		t.Fatalf("state = %q, want pending for an operator", response.State)
	}
	if response.PIN != nil {
		t.Fatalf("a mismatched settlement produced a PIN: %v", response.PIN)
	}
}

// TestReconciliation_ClosesAPaymentTheGatewayReportsFailed covers the other half
// of the mapping: a vendor that reports `failed` closes the pending Transaction
// through the same guarded transition a callback uses.
func TestReconciliation_ClosesAPaymentTheGatewayReportsFailed(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()
	cookie := newSessionCookie(t, harness, "081300000055", "198.51.100.55")
	created := checkout(t, harness, cookie, firstPackageID(t, harness), "BRIVA")

	harness.Doubles.Gateway.Statuses[created.Reference] = payment.PaymentStatus{
		Reference: created.Reference,
		Status:    payment.StatusFailed,
		Amount:    created.AmountIDR,
	}

	reconciled, err := harness.TransactionReconciliation.ReconcileDue(ctx)
	if err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}
	if reconciled != 1 {
		t.Fatalf("reconciled %d Transactions, want 1", reconciled)
	}
	if state := getTransaction(t, harness, cookie, created.ID).State; state != string(transactionentity.StateFailed) {
		t.Fatalf("state = %q, want failed", state)
	}
	if code := postCallback(t, harness, created.Reference, payment.StatusPaid, created.AmountIDR,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("late settlement = %d, want 200 (acknowledged, ignored)", code)
	}
	if state := getTransaction(t, harness, cookie, created.ID).State; state != string(transactionentity.StateFailed) {
		t.Fatalf("failed → paid was allowed: state = %q", state)
	}
}
