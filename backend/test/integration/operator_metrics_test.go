package integration

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	notificationentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	notificationrepo "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/repository"
	notificationservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	transactionentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	transactionrepo "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestOperatorCounters_ReportTheBacklogs is the ADR 0018 metrics contract against
// a real database: the outbox answers per state plus the longest wait, and
// provisioning answers how many paid rows are owed and what they have cost.
func TestOperatorCounters_ReportTheBacklogs(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()

	outbox := notificationservice.NewOutboxCounter(
		notificationrepo.NewRepository(harness.Postgres, zap.NewNop()),
		harness.Doubles.Clock,
	)
	provisioning := transactionservice.NewProvisioningCounter(
		transactionrepo.NewRepository(harness.Postgres, zap.NewNop()),
	)

	now := harness.Doubles.Clock.Now()

	// One row per state. The oldest undelivered message is the failed one, 90
	// seconds old; the sent row is older still and must not count towards the wait.
	for _, row := range []*notificationentity.SMS{
		{
			PhoneNumber: "6281300000071", Message: "PIN Anda", State: notificationentity.StatePending,
			NextAttemptAt: now, CreatedAt: now.Add(-30 * time.Second),
		},
		{
			PhoneNumber: "6281300000071", Message: "PIN Anda", State: notificationentity.StateSending,
			NextAttemptAt: now, CreatedAt: now.Add(-45 * time.Second),
		},
		{
			PhoneNumber: "6281300000071", Message: "PIN Anda", State: notificationentity.StateFailed,
			NextAttemptAt: now, CreatedAt: now.Add(-90 * time.Second),
		},
		{
			PhoneNumber: "6281300000071", Message: "", State: notificationentity.StateSent,
			NextAttemptAt: now, CreatedAt: now.Add(-10 * time.Minute), SentAt: &now,
		},
	} {
		if err := harness.Postgres.Create(row).Error; err != nil {
			t.Fatalf("seed sms row: %v", err)
		}
	}

	// Two paid rows are still owed; a provisioned row and a pending one are not part
	// of the backlog, and the attempts of a finished row do not count.
	stillOwed := newTransaction("TRX-OWED-1", "6281300000071", 1, transactionentity.StatePaid)
	stillOwed.ProvisionAttempts = 2
	alsoOwed := newTransaction("TRX-OWED-2", "6281300000072", 2, transactionentity.StatePaid)
	done := newTransaction("TRX-DONE", "6281300000073", 3, transactionentity.StateProvisioned)
	done.ProvisionAttempts = 9
	waitingPayment := newTransaction("TRX-PENDING", "6281300000074", 4, transactionentity.StatePending)

	for _, row := range []*transactionentity.Transaction{stillOwed, alsoOwed, done, waitingPayment} {
		if err := harness.Postgres.Create(row).Error; err != nil {
			t.Fatalf("seed %s: %v", row.Reference, err)
		}
	}

	outboxCounts, err := outbox.CountOutbox(ctx)
	if err != nil {
		t.Fatalf("CountOutbox: %v", err)
	}
	if outboxCounts.Pending != 1 || outboxCounts.Sending != 1 ||
		outboxCounts.Sent != 1 || outboxCounts.Failed != 1 {
		t.Fatalf("outbox counts = %+v, want one row per state", outboxCounts)
	}
	if outboxCounts.OldestPendingAgeSeconds != 90 {
		t.Fatalf("oldest wait = %ds, want 90 (the failed row, not the delivered one)",
			outboxCounts.OldestPendingAgeSeconds)
	}

	provisioningCounts, err := provisioning.CountProvisioning(ctx)
	if err != nil {
		t.Fatalf("CountProvisioning: %v", err)
	}
	if provisioningCounts.PaidAwaiting != 2 {
		t.Fatalf("paid awaiting = %d, want 2", provisioningCounts.PaidAwaiting)
	}
	if provisioningCounts.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (only rows still waiting)", provisioningCounts.Attempts)
	}

	// Once the backlog clears both numbers go back to zero: they are gauges, not
	// counters that only ever grow.
	if err := harness.Postgres.Model(&transactionentity.Transaction{}).
		Where("state = ?", transactionentity.StatePaid).
		Update("state", transactionentity.StateProvisioned).Error; err != nil {
		t.Fatalf("clear the backlog: %v", err)
	}

	cleared, err := provisioning.CountProvisioning(ctx)
	if err != nil {
		t.Fatalf("CountProvisioning after clearing: %v", err)
	}
	if cleared.PaidAwaiting != 0 || cleared.Attempts != 0 {
		t.Fatalf("counts after clearing = %+v, want 0/0", cleared)
	}
}
