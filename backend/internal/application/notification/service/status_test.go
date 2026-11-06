package service_test

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
)

// TestStatusReader_AnswersTheNewestRowPerPhoneNumber is the panel's one read of the
// outbox: one row per number, the new one winning, and a number that was never
// messaged simply absent (so the panel shows no status rather than a wrong one).
func TestStatusReader_AnswersTheNewestRowPerPhoneNumber(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)
	repo := &fakeOutboxRepository{rows: []entity.SMS{
		{
			PhoneNumber: "6281200000001", State: entity.StateFailed,
			NextAttemptAt: now, UpdatedAt: now.Add(-5 * time.Minute),
		},
		{
			PhoneNumber: "6281200000001", State: entity.StateSent,
			NextAttemptAt: now, UpdatedAt: now,
		},
		{
			PhoneNumber: "6281200000002", State: entity.StatePending,
			NextAttemptAt: now, UpdatedAt: now,
		},
	}}

	reader := service.NewStatusReader(repo, zap.NewNop())
	statuses, err := reader.LatestStatuses(context.Background(), []string{
		"6281200000001", "6281200000001", "6281200000002", "6281200000003", "",
	})
	if err != nil {
		t.Fatalf("LatestStatuses: %v", err)
	}

	if len(statuses) != 2 {
		t.Fatalf("statuses = %d, want 2", len(statuses))
	}
	if got := statuses["6281200000001"].State; got != string(entity.StateSent) {
		t.Fatalf("first number = %q, want the newest row's state sent", got)
	}
	if !statuses["6281200000001"].UpdatedAt.Equal(now) {
		t.Fatalf("first number updated_at = %v, want %v", statuses["6281200000001"].UpdatedAt, now)
	}
	if got := statuses["6281200000002"].State; got != string(entity.StatePending) {
		t.Fatalf("second number = %q, want pending", got)
	}
	if _, present := statuses["6281200000003"]; present {
		t.Fatal("a Phone Number that was never messaged must be absent, not a fabricated status")
	}
}

func TestStatusReader_DoesNotQueryForAnEmptyPage(t *testing.T) {
	t.Parallel()

	reader := service.NewStatusReader(&fakeOutboxRepository{}, zap.NewNop())
	statuses, err := reader.LatestStatuses(context.Background(), nil)
	if err != nil {
		t.Fatalf("LatestStatuses: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("statuses = %v, want none", statuses)
	}
}
