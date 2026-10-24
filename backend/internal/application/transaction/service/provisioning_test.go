package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	notificationservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/pin"
)

// packageDuration is the Active Period of the fake catalog's Package 7.
const packageDuration = 10080 * time.Minute

type fakeRadiusWriter struct {
	accounts []provisioning.Account
	err      error
}

func (w *fakeRadiusWriter) Apply(_ context.Context, account provisioning.Account) error {
	if w.err != nil {
		return w.err
	}
	w.accounts = append(w.accounts, account)
	return nil
}

type fakeRadiusReader struct {
	expiration provisioning.Expiration
	err        error
}

func (r *fakeRadiusReader) Expiration(context.Context, string) (provisioning.Expiration, error) {
	if r.err != nil {
		return provisioning.Expiration{}, r.err
	}
	return r.expiration, nil
}

type fakeSMSOutbox struct {
	messages []notificationservice.Message
	err      error
}

func (o *fakeSMSOutbox) Enqueue(ctx context.Context, message notificationservice.Message) error {
	return o.EnqueueTx(ctx, database.Tx{}, message)
}

func (o *fakeSMSOutbox) EnqueueTx(
	ctx context.Context,
	_ database.Tx,
	message notificationservice.Message,
) error {
	_ = ctx
	if o.err != nil {
		return o.err
	}
	o.messages = append(o.messages, message)
	return nil
}

// addPaid appends the row a settlement would leave behind.
func (f *fixture) addPaid(t *testing.T) *entity.Transaction {
	t.Helper()

	paidAt := f.clock.now
	row := entity.Transaction{
		ID:          uint(len(f.repo.rows) + 1),
		Reference:   "TRX-PROVISION-TEST",
		PhoneNumber: testPhone,
		PackageID:   7,
		PackageName: "Paket Mingguan",
		AmountIDR:   25000,
		Channel:     "BRIVA",
		State:       entity.StatePaid,
		ExpiresAt:   paidAt.Add(time.Hour),
		PaidAt:      &paidAt,
		CreatedAt:   paidAt,
		UpdatedAt:   paidAt,
	}
	f.repo.rows = append(f.repo.rows, row)

	return &f.repo.rows[len(f.repo.rows)-1]
}

func TestProvision_FirstPurchaseWritesTheAccountAndQueuesThePIN(t *testing.T) {
	t.Parallel()

	f := newFixture()
	paidAt := f.clock.now
	row := f.addPaid(t)

	if err := f.provisioner.Provision(context.Background(), row.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if len(f.writer.accounts) != 1 {
		t.Fatalf("writer applied %d accounts, want 1", len(f.writer.accounts))
	}
	account := f.writer.accounts[0]
	if account.PhoneNumber != testPhone {
		t.Fatalf("username = %q, want the canonical Phone Number", account.PhoneNumber)
	}
	if len(account.PIN) != pin.Length {
		t.Fatalf("PIN %q is not %d digits", account.PIN, pin.Length)
	}
	if account.RateLimit != "10M/10M" {
		t.Fatalf("rate limit = %q, want the Package's", account.RateLimit)
	}
	if want := paidAt.Add(packageDuration); !account.Expiration.Equal(want) {
		t.Fatalf("expiration = %s, want settlement + duration (%s)", account.Expiration, want)
	}

	if f.repo.rows[0].State != entity.StateProvisioned {
		t.Fatalf("state = %q, want provisioned", f.repo.rows[0].State)
	}
	if len(f.outbox.messages) != 1 {
		t.Fatalf("queued %d SMS, want 1", len(f.outbox.messages))
	}
	if !strings.Contains(f.outbox.messages[0].Text, account.PIN) {
		t.Fatalf("SMS %q does not carry the PIN", f.outbox.messages[0].Text)
	}
	if f.outbox.messages[0].PhoneNumber != testPhone {
		t.Fatalf("SMS goes to %q, want the buyer", f.outbox.messages[0].PhoneNumber)
	}
}

func TestProvision_ExtendAddsToTheRunningActivePeriod(t *testing.T) {
	t.Parallel()

	f := newFixture()
	currentExpiration := f.clock.now.Add(48 * time.Hour)
	f.reader.expiration = provisioning.Expiration{ExpiresAt: currentExpiration, Found: true}
	row := f.addPaid(t)

	if err := f.provisioner.Provision(context.Background(), row.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if want := currentExpiration.Add(packageDuration); !f.writer.accounts[0].Expiration.Equal(want) {
		t.Fatalf("expiration = %s, want the running Active Period + duration (%s)",
			f.writer.accounts[0].Expiration, want)
	}
}

func TestProvision_FinishedActivePeriodStartsAtSettlement(t *testing.T) {
	t.Parallel()

	f := newFixture()
	paidAt := f.clock.now
	// A later clock reading than the settlement must not shift the start: PRD
	// point 21 says settlement time, not "when the worker happened to run".
	f.reader.expiration = provisioning.Expiration{ExpiresAt: paidAt.Add(-time.Hour), Found: true}
	row := f.addPaid(t)
	f.clock.Advance(30 * time.Minute)

	if err := f.provisioner.Provision(context.Background(), row.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if want := paidAt.Add(packageDuration); !f.writer.accounts[0].Expiration.Equal(want) {
		t.Fatalf("expiration = %s, want settlement + duration (%s)",
			f.writer.accounts[0].Expiration, want)
	}
}

func TestProvision_RetryKeepsTheDurationAndThePIN(t *testing.T) {
	t.Parallel()

	f := newFixture()
	row := f.addPaid(t)
	ctx := context.Background()

	// The first attempt writes FreeRADIUS and then loses Postgres, which is the
	// crash the retry exists for.
	f.outbox.err = errors.New("postgres unavailable")
	if err := f.provisioner.Provision(ctx, row.ID); err == nil {
		t.Fatal("first Provision succeeded although the SMS could not be queued")
	}
	if f.repo.rows[0].State != entity.StatePaid {
		t.Fatalf("state after the failed attempt = %q, want paid", f.repo.rows[0].State)
	}

	f.outbox.err = nil
	if err := f.provisioner.Provision(ctx, row.ID); err != nil {
		t.Fatalf("retry Provision: %v", err)
	}

	if len(f.writer.accounts) != 2 {
		t.Fatalf("writer applied %d accounts, want 2 (one per attempt)", len(f.writer.accounts))
	}
	if f.writer.accounts[0] != f.writer.accounts[1] {
		t.Fatalf("the retry wrote a different account: %+v vs %+v",
			f.writer.accounts[0], f.writer.accounts[1])
	}
	if len(f.outbox.messages) != 1 {
		t.Fatalf("queued %d SMS, want exactly 1", len(f.outbox.messages))
	}
	if f.repo.rows[0].State != entity.StateProvisioned {
		t.Fatalf("state = %q, want provisioned", f.repo.rows[0].State)
	}
}

func TestProvision_LeavesTransactionsThatAreNotPaidAlone(t *testing.T) {
	t.Parallel()

	f := newFixture()
	row := f.addPaid(t)
	f.repo.rows[0].State = entity.StatePending

	if err := f.provisioner.Provision(context.Background(), row.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(f.writer.accounts) != 0 {
		t.Fatalf("writer applied %d accounts for a pending Transaction", len(f.writer.accounts))
	}
	if len(f.outbox.messages) != 0 {
		t.Fatalf("queued %d SMS for a pending Transaction", len(f.outbox.messages))
	}
}

func TestProvision_UnknownTransactionIsNotAnError(t *testing.T) {
	t.Parallel()

	f := newFixture()
	if err := f.provisioner.Provision(context.Background(), 999); err != nil {
		t.Fatalf("Provision on a missing Transaction: %v", err)
	}
}

func TestProvision_FailingOutboxLeavesTheTransactionPaid(t *testing.T) {
	t.Parallel()

	f := newFixture()
	row := f.addPaid(t)
	f.outbox.err = errors.New("postgres unavailable")

	if err := f.provisioner.Provision(context.Background(), row.ID); err == nil {
		t.Fatal("Provision succeeded although the SMS could not be queued")
	}
	if f.repo.rows[0].State != entity.StatePaid {
		t.Fatalf("state = %q, want the Transaction to stay paid for a retry",
			f.repo.rows[0].State)
	}
}

func TestProvision_FailingExpirationReadKeepsTheTransactionPaid(t *testing.T) {
	t.Parallel()

	f := newFixture()
	row := f.addPaid(t)
	f.reader.err = errors.New("mysql down")

	if err := f.provisioner.Provision(context.Background(), row.ID); err == nil {
		t.Fatal("Provision succeeded without the current Active Period")
	}
	if len(f.writer.accounts) != 0 {
		t.Fatal("the account was written although the target expiration is unknown")
	}
	if f.repo.rows[0].ProvisionTargetExpiration != nil {
		t.Fatal("a failed attempt stored a provisioning plan")
	}
}

func TestProvisionDue_ProvisionsEveryPaidTransaction(t *testing.T) {
	t.Parallel()

	f := newFixture()
	first := f.addPaid(t)
	f.repo.rows = append(f.repo.rows, entity.Transaction{
		ID:          first.ID + 1,
		Reference:   "TRX-PROVISION-SECOND",
		PhoneNumber: "6281200000002",
		PackageID:   7,
		PackageName: "Paket Mingguan",
		AmountIDR:   25000,
		Channel:     "BRIVA",
		State:       entity.StatePaid,
		ExpiresAt:   f.clock.now.Add(time.Hour),
		PaidAt:      &f.clock.now,
	})

	done, err := f.provisioner.ProvisionDue(context.Background())
	if err != nil {
		t.Fatalf("ProvisionDue: %v", err)
	}
	if done != 2 {
		t.Fatalf("provisioned %d, want 2", done)
	}
}

func TestCurrentPIN_ReturnsTheNewestProvisionedPIN(t *testing.T) {
	t.Parallel()

	f := newFixture()
	ctx := context.Background()

	if _, found, err := f.service.CurrentPIN(ctx, testPhone); err != nil || found {
		t.Fatalf("CurrentPIN before a purchase = (%v, %v), want not found", found, err)
	}

	row := f.addPaid(t)
	if err := f.provisioner.Provision(ctx, row.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	current, found, err := f.service.CurrentPIN(ctx, testPhone)
	if err != nil {
		t.Fatalf("CurrentPIN: %v", err)
	}
	if !found {
		t.Fatal("CurrentPIN did not find the provisioned Transaction's PIN")
	}
	if current != f.writer.accounts[0].PIN {
		t.Fatalf("CurrentPIN = %q, want the PIN written to FreeRADIUS %q",
			current, f.writer.accounts[0].PIN)
	}
}
