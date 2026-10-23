package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
)

// --- fakes -----------------------------------------------------------------

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeOutboxRepository struct {
	rows   []entity.SMS
	nextID uint
}

func (r *fakeOutboxRepository) Enqueue(_ context.Context, message *entity.SMS) error {
	r.nextID++
	message.ID = r.nextID
	r.rows = append(r.rows, *message)
	return nil
}

func (r *fakeOutboxRepository) ClaimDue(_ context.Context, now time.Time, _ time.Duration) (*entity.SMS, error) {
	for i := range r.rows {
		row := &r.rows[i]
		if row.State != entity.StatePending || row.NextAttemptAt.After(now) {
			continue
		}
		row.State = entity.StateSending
		row.Attempts++
		clone := *row
		return &clone, nil
	}
	return nil, nil
}

func (r *fakeOutboxRepository) MarkSent(_ context.Context, id uint, sentAt time.Time) error {
	for i := range r.rows {
		if r.rows[i].ID == id {
			r.rows[i].State = entity.StateSent
			r.rows[i].SentAt = &sentAt
		}
	}
	return nil
}

func (r *fakeOutboxRepository) MarkFailed(
	_ context.Context,
	id uint,
	nextAttemptAt *time.Time,
	cause string,
	now time.Time,
) error {
	for i := range r.rows {
		if r.rows[i].ID != id {
			continue
		}
		r.rows[i].LastError = cause
		if nextAttemptAt == nil {
			r.rows[i].State = entity.StateFailed
			continue
		}
		r.rows[i].State = entity.StatePending
		r.rows[i].NextAttemptAt = *nextAttemptAt
		r.rows[i].UpdatedAt = now
	}
	return nil
}

type fakeSender struct {
	sent []string
	err  error
}

func (s *fakeSender) Send(_ context.Context, phoneNumber, message string) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, phoneNumber+"|"+message)
	return nil
}

type recordingWaker struct {
	tasks []string
	err   error
}

func (w *recordingWaker) Enqueue(_ context.Context, taskType string) error {
	w.tasks = append(w.tasks, taskType)
	return w.err
}

// --- tests -----------------------------------------------------------------

func newOutbox(repo repository.Repository, waker *recordingWaker) (service.Outbox, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
	return service.NewOutbox(repo, clock, waker, zap.NewNop()), clock
}

func newDeliverer(
	repo repository.Repository,
	sender *fakeSender,
	clock *fakeClock,
	maxAttempts int,
) service.Deliverer {
	return service.NewDeliverer(repo, sender, clock, service.Settings{
		Lease:        time.Minute,
		MaxAttempts:  maxAttempts,
		RetryBackoff: time.Minute,
	}, zap.NewNop())
}

func TestEnqueueStoresARowAndWakesTheWorker(t *testing.T) {
	repo := &fakeOutboxRepository{}
	waker := &recordingWaker{}
	outbox, _ := newOutbox(repo, waker)

	if err := outbox.Enqueue(context.Background(), service.Message{
		PhoneNumber: "6281234567890",
		Text:        "PIN: 123456",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if len(repo.rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(repo.rows))
	}
	if repo.rows[0].State != entity.StatePending || repo.rows[0].Message != "PIN: 123456" {
		t.Fatalf("row = %+v", repo.rows[0])
	}
	if len(waker.tasks) != 1 {
		t.Fatalf("enqueued %d wake-up tasks, want 1", len(waker.tasks))
	}
}

func TestEnqueueKeepsTheRowWhenTheWakeUpFails(t *testing.T) {
	repo := &fakeOutboxRepository{}
	waker := &recordingWaker{err: errors.New("redis is down")}
	outbox, _ := newOutbox(repo, waker)

	if err := outbox.Enqueue(context.Background(), service.Message{
		PhoneNumber: "6281234567890",
		Text:        "PIN: 123456",
	}); err != nil {
		t.Fatalf("a Redis hiccup must not fail the enqueue: %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatal("the row is the durable record and must survive a lost wake-up")
	}
}

func TestEnqueueRejectsAnEmptyMessage(t *testing.T) {
	outbox, _ := newOutbox(&fakeOutboxRepository{}, &recordingWaker{})

	if err := outbox.Enqueue(context.Background(), service.Message{}); err == nil {
		t.Fatal("expected an error for a message without a Phone Number or text")
	}
}

func TestDeliverDueSendsAndClosesTheRow(t *testing.T) {
	repo := &fakeOutboxRepository{}
	outbox, clock := newOutbox(repo, &recordingWaker{})
	sender := &fakeSender{}

	if err := outbox.Enqueue(context.Background(), service.Message{
		PhoneNumber: "6281234567890",
		Text:        "PIN: 123456",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	delivered, err := newDeliverer(repo, sender, clock, 5).DeliverDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered %d messages, want 1", delivered)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "6281234567890|PIN: 123456" {
		t.Fatalf("sender recorded %v", sender.sent)
	}
	if repo.rows[0].State != entity.StateSent {
		t.Fatalf("row state = %q, want sent", repo.rows[0].State)
	}
}

func TestDeliverDueSchedulesARetryWhenTheModemFails(t *testing.T) {
	repo := &fakeOutboxRepository{}
	outbox, clock := newOutbox(repo, &recordingWaker{})
	sender := &fakeSender{err: errors.New("no carrier")}

	if err := outbox.Enqueue(context.Background(), service.Message{
		PhoneNumber: "6281234567890",
		Text:        "PIN: 123456",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	delivered, err := newDeliverer(repo, sender, clock, 5).DeliverDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("a modem failure must be recorded, not returned: %v", err)
	}
	if delivered != 0 {
		t.Fatalf("delivered %d messages, want 0", delivered)
	}

	row := repo.rows[0]
	if row.State != entity.StatePending {
		t.Fatalf("row state = %q, want pending", row.State)
	}
	if row.LastError != "no carrier" {
		t.Fatalf("last_error = %q", row.LastError)
	}
	if got := row.NextAttemptAt.Sub(clock.now); got != time.Minute {
		t.Fatalf("retry scheduled in %v, want 1m", got)
	}
}

func TestDeliverDueGivesUpAfterTheLastAttempt(t *testing.T) {
	repo := &fakeOutboxRepository{}
	outbox, clock := newOutbox(repo, &recordingWaker{})
	sender := &fakeSender{err: errors.New("no carrier")}
	deliverer := newDeliverer(repo, sender, clock, 1)

	if err := outbox.Enqueue(context.Background(), service.Message{
		PhoneNumber: "6281234567890",
		Text:        "PIN: 123456",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if _, err := deliverer.DeliverDue(context.Background(), 10); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	row := repo.rows[0]
	if row.State != entity.StateFailed {
		t.Fatalf("row state = %q, want failed after the last attempt", row.State)
	}
}

func TestDeliverDueDoesNothingWhenNoRowIsDue(t *testing.T) {
	repo := &fakeOutboxRepository{}
	_, clock := newOutbox(repo, &recordingWaker{})
	sender := &fakeSender{}

	delivered, err := newDeliverer(repo, sender, clock, 5).DeliverDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if delivered != 0 || len(sender.sent) != 0 {
		t.Fatalf("delivered %d with %d sends, want none", delivered, len(sender.sent))
	}
}

func TestDeliverDueSendsAfterTheModemRecovers(t *testing.T) {
	repo := &fakeOutboxRepository{}
	outbox, clock := newOutbox(repo, &recordingWaker{})
	sender := &fakeSender{err: errors.New("no carrier")}
	deliverer := newDeliverer(repo, sender, clock, 5)

	if err := outbox.Enqueue(context.Background(), service.Message{
		PhoneNumber: "6281234567890",
		Text:        "PIN: 123456",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// The modem is off for the first attempt: the message must stay pending.
	if _, err := deliverer.DeliverDue(context.Background(), 10); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if repo.rows[0].State != entity.StatePending {
		t.Fatalf("row state = %q, want pending while the modem is unavailable", repo.rows[0].State)
	}

	// The modem comes back and the backoff window passes.
	sender.err = nil
	clock.now = repo.rows[0].NextAttemptAt.Add(time.Second)

	delivered, err := deliverer.DeliverDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("deliver after recovery: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered %d messages after recovery, want 1", delivered)
	}
	if repo.rows[0].State != entity.StateSent {
		t.Fatalf("row state = %q, want sent after recovery", repo.rows[0].State)
	}
}
