package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
)

// observedFixture is the ordinary fixture with a captured logger, so a test can
// assert the event contract an operator queries instead of the prose next to it
// (ADR 0018).
func observedFixture(t *testing.T) (*fixture, *observer.ObservedLogs) {
	t.Helper()

	core, logs := observer.New(zap.InfoLevel)
	return newFixtureWithLogger(zap.New(core)), logs
}

func entriesFor(logs *observer.ObservedLogs, event string) []observer.LoggedEntry {
	matched := make([]observer.LoggedEntry, 0, 1)
	for _, entry := range logs.All() {
		if entry.ContextMap()["event"] == event {
			matched = append(matched, entry)
		}
	}
	return matched
}

func onlyEntry(t *testing.T, logs *observer.ObservedLogs, event string) map[string]any {
	t.Helper()

	matched := entriesFor(logs, event)
	if len(matched) != 1 {
		t.Fatalf("%s logged %d times, want exactly 1", event, len(matched))
	}
	return matched[0].ContextMap()
}

func TestStateChange_CallbackLogsReasonAndMasksThePhoneNumber(t *testing.T) {
	t.Parallel()

	f, logs := observedFixture(t)
	transaction := f.checkout(t, validRequest())
	raw, signature := f.callback(t, transaction.Reference, payment.StatusPaid, transaction.AmountIDR)

	if err := f.service.HandleCallback(context.Background(), raw, signature); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}

	fields := onlyEntry(t, logs, service.EventStateChanged)
	if fields["reason"] != service.ReasonGatewayCallback {
		t.Fatalf("reason = %v, want %q", fields["reason"], service.ReasonGatewayCallback)
	}
	if fields["from"] != string(entity.StatePending) || fields["to"] != string(entity.StatePaid) {
		t.Fatalf("transition logged as %v → %v, want pending → paid", fields["from"], fields["to"])
	}
	if fields["reference"] != transaction.Reference {
		t.Fatalf("reference = %v, want %q", fields["reference"], transaction.Reference)
	}
	if fields["transaction_id"] != uint64(transaction.ID) {
		t.Fatalf("transaction_id = %v, want %d", fields["transaction_id"], transaction.ID)
	}
	if fields["amount_idr"] != transaction.AmountIDR {
		t.Fatalf("amount_idr = %v, want %d", fields["amount_idr"], transaction.AmountIDR)
	}

	// The Phone Number is the account identity: it appears masked and never raw.
	if fields["phone"] != phone.Mask(testPhone) {
		t.Fatalf("phone = %v, want the masked %q", fields["phone"], phone.Mask(testPhone))
	}
	if fields["phone"] == testPhone {
		t.Fatal("the raw Phone Number reached a log line")
	}
}

func TestStateChange_ReconciliationIsDistinguishableFromACallback(t *testing.T) {
	t.Parallel()

	f, logs := observedFixture(t)
	transaction := f.checkout(t, validRequest())
	f.gateway.statuses[transaction.Reference] = settledStatus(transaction.Reference, transaction.AmountIDR, f.clock.Now())

	if _, err := f.reconciler.ReconcileDue(context.Background()); err != nil {
		t.Fatalf("ReconcileDue: %v", err)
	}

	fields := onlyEntry(t, logs, service.EventStateChanged)
	if fields["reason"] != service.ReasonReconciliation {
		t.Fatalf("reason = %v, want %q", fields["reason"], service.ReasonReconciliation)
	}
}

func TestStateChange_ExpirySweepNamesEveryRowItClosed(t *testing.T) {
	t.Parallel()

	f, logs := observedFixture(t)
	first := f.checkout(t, validRequest())
	f.clock.Advance(2 * time.Hour)

	if _, err := service.NewExpiry(f.repo, f.clock, f.logger).ExpireDue(context.Background()); err != nil {
		t.Fatalf("ExpireDue: %v", err)
	}

	fields := onlyEntry(t, logs, service.EventStateChanged)
	if fields["reason"] != service.ReasonExpirySweep {
		t.Fatalf("reason = %v, want %q", fields["reason"], service.ReasonExpirySweep)
	}
	if fields["from"] != string(entity.StatePending) || fields["to"] != string(entity.StateExpired) {
		t.Fatalf("transition logged as %v → %v, want pending → expired", fields["from"], fields["to"])
	}
	if fields["transaction_id"] != uint64(first.ID) {
		t.Fatalf("transaction_id = %v, want the expired %d", fields["transaction_id"], first.ID)
	}
}

// TestStateChange_CheckoutSupersedeIsItsOwnReason covers the fifth reason: the
// buyer's next checkout closing a deadline the sweep had not reached yet.
func TestStateChange_CheckoutSupersedeIsItsOwnReason(t *testing.T) {
	t.Parallel()

	f, logs := observedFixture(t)
	first := f.checkout(t, validRequest())
	f.clock.Advance(2 * time.Hour)
	f.checkout(t, validRequest())

	fields := onlyEntry(t, logs, service.EventStateChanged)
	if fields["reason"] != service.ReasonCheckoutSuperseded {
		t.Fatalf("reason = %v, want %q", fields["reason"], service.ReasonCheckoutSuperseded)
	}
	if fields["transaction_id"] != uint64(first.ID) {
		t.Fatalf("transaction_id = %v, want the superseded %d", fields["transaction_id"], first.ID)
	}
}

func TestProvisioningAttempt_IsLoggedWithItsCountAndOutcome(t *testing.T) {
	t.Parallel()

	f, logs := observedFixture(t)
	transaction := f.checkout(t, validRequest())
	raw, signature := f.callback(t, transaction.Reference, payment.StatusPaid, transaction.AmountIDR)
	if err := f.service.HandleCallback(context.Background(), raw, signature); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	logs.TakeAll() // the settlement's own transition is asserted above

	if err := f.provisioner.Provision(context.Background(), transaction.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	attempt := onlyEntry(t, logs, service.EventProvisioningAttempt)
	if attempt["attempt"] != int64(1) {
		t.Fatalf("attempt = %v, want 1 on the first try", attempt["attempt"])
	}
	if attempt["outcome"] != service.OutcomeOK {
		t.Fatalf("outcome = %v, want %q", attempt["outcome"], service.OutcomeOK)
	}
	// The attempt names the buyer itself, masked, so an operator never has to join
	// it to the state-change line by an unmasked identifier (ADR 0018 §4).
	if attempt["phone"] != phone.Mask(testPhone) {
		t.Fatalf("phone = %v, want the masked %q", attempt["phone"], phone.Mask(testPhone))
	}
	if attempt["phone"] == testPhone {
		t.Fatal("the raw Phone Number reached the provisioning attempt log")
	}

	changed := onlyEntry(t, logs, service.EventStateChanged)
	if changed["reason"] != service.ReasonProvisioning {
		t.Fatalf("reason = %v, want %q", changed["reason"], service.ReasonProvisioning)
	}
	if changed["from"] != string(entity.StatePaid) || changed["to"] != string(entity.StateProvisioned) {
		t.Fatalf("transition logged as %v → %v, want paid → provisioned", changed["from"], changed["to"])
	}
}

// TestProvisioningAttempt_FailureIsLoggedAsARetry covers the outcome vocabulary:
// there is no `failed`, because a paid Transaction is retried until it works.
func TestProvisioningAttempt_FailureIsLoggedAsARetry(t *testing.T) {
	t.Parallel()

	f, logs := observedFixture(t)
	transaction := f.checkout(t, validRequest())
	raw, signature := f.callback(t, transaction.Reference, payment.StatusPaid, transaction.AmountIDR)
	if err := f.service.HandleCallback(context.Background(), raw, signature); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}

	f.writer.err = errors.New("freeradius: connection refused")
	if err := f.provisioner.Provision(context.Background(), transaction.ID); err == nil {
		t.Fatal("Provision reported success while the writer was down")
	}
	first := onlyEntry(t, logs, service.EventProvisioningAttempt)
	if first["outcome"] != service.OutcomeRetry || first["attempt"] != int64(1) {
		t.Fatalf("first attempt = %v/%v, want retry/1", first["outcome"], first["attempt"])
	}
	if first["phone"] != phone.Mask(testPhone) {
		t.Fatalf("phone = %v, want the masked %q", first["phone"], phone.Mask(testPhone))
	}
	if f.repo.rows[0].State != entity.StatePaid {
		t.Fatalf("state = %q, want the row still paid for the next sweep", f.repo.rows[0].State)
	}

	logs.TakeAll()
	f.writer.err = nil
	if err := f.provisioner.Provision(context.Background(), transaction.ID); err != nil {
		t.Fatalf("second Provision: %v", err)
	}
	second := onlyEntry(t, logs, service.EventProvisioningAttempt)
	if second["outcome"] != service.OutcomeOK || second["attempt"] != int64(2) {
		t.Fatalf("second attempt = %v/%v, want ok/2", second["outcome"], second["attempt"])
	}
}
