package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
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

func (r *fakeOutboxRepository) EnqueueTx(
	ctx context.Context,
	_ database.Tx,
	message *entity.SMS,
) error {
	return r.Enqueue(ctx, message)
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
			r.rows[i].Message = ""
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
			r.rows[i].Message = ""
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

// A body over one GSM-7 segment is refused by the modem with a bare ERROR (178
// characters of Magic Link copy did exactly that in production), so the outbox
// refuses it before a row exists: nothing to retry, nothing to leave behind.
func TestEnqueueRejectsABodyLongerThanOneSegment(t *testing.T) {
	repo := &fakeOutboxRepository{}
	waker := &recordingWaker{}
	outbox, _ := newOutbox(repo, waker)

	err := outbox.Enqueue(context.Background(), service.Message{
		PhoneNumber: "6281234567890",
		Text:        strings.Repeat("a", 178),
	})
	if !errors.Is(err, sms.ErrTooLong) {
		t.Fatalf("enqueue = %v, want ErrTooLong", err)
	}
	if len(repo.rows) != 0 {
		t.Fatalf("stored %d rows for an undeliverable body, want none", len(repo.rows))
	}
	if len(waker.tasks) != 0 {
		t.Fatalf("woke the worker %d times for an undeliverable body", len(waker.tasks))
	}
}

func TestEnqueueRejectsAnUnencodableBody(t *testing.T) {
	outbox, _ := newOutbox(&fakeOutboxRepository{}, &recordingWaker{})

	err := outbox.Enqueue(context.Background(), service.Message{
		PhoneNumber: "6281234567890",
		Text:        "PIN: 123456 \U0001F680",
	})
	if !errors.Is(err, sms.ErrUnencodable) {
		t.Fatalf("enqueue = %v, want ErrUnencodable", err)
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
	if repo.rows[0].Message != "" {
		t.Fatalf("a sent row kept its body %q; a delivered PIN must not stay in Postgres",
			repo.rows[0].Message)
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
	if row.Message != "PIN: 123456" {
		t.Fatalf("a retry lost its body: %q", row.Message)
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
	if row.Message != "" {
		t.Fatalf("a row that will never retry kept its body %q", row.Message)
	}
}

func TestDeliverDueDoesNotRetryAnUnsendableBody(t *testing.T) {
	// The outbox now refuses such a body, but rows written before the rule
	// existed (a 178-character Magic Link in sms_outbox) still reach the drainer.
	repo := &fakeOutboxRepository{}
	_, clock := newOutbox(repo, &recordingWaker{})
	sender := &fakeSender{err: &sms.Unsendable{Reason: sms.ErrTooLong, Septets: 178}}
	deliverer := newDeliverer(repo, sender, clock, 5)

	repo.rows = append(repo.rows, entity.SMS{
		ID:            7,
		PhoneNumber:   "6281234567890",
		Message:       strings.Repeat("a", 178),
		State:         entity.StatePending,
		NextAttemptAt: clock.now,
	})

	delivered, err := deliverer.DeliverDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if delivered != 0 {
		t.Fatalf("delivered %d messages, want 0", delivered)
	}

	row := repo.rows[0]
	if row.State != entity.StateFailed {
		t.Fatalf("row state = %q, want failed without a retry", row.State)
	}
	if row.Message != "" {
		t.Fatalf("a row that will never be retried kept its body (%d chars)", len(row.Message))
	}
	if !strings.Contains(row.LastError, "septets") {
		t.Fatalf("last_error = %q, want the reason the body is unsendable", row.LastError)
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

// Counts mirrors the repository's one-statement count so a test can assert the
// numbers the operator surface publishes (ADR 0018).
func (r *fakeOutboxRepository) Counts(_ context.Context, now time.Time) (repository.Counts, error) {
	counts := repository.Counts{}
	for _, row := range r.rows {
		switch row.State {
		case entity.StatePending:
			counts.Pending++
		case entity.StateSending:
			counts.Sending++
		case entity.StateSent:
			counts.Sent++
		case entity.StateFailed:
			counts.Failed++
		}
		// Only rows still owed to a buyer count: a terminal sent/failed row is not a
		// wait, so the gauge can return to zero (ADR 0018).
		if row.State != entity.StatePending && row.State != entity.StateSending {
			continue
		}
		age := int64(now.Sub(row.CreatedAt).Seconds())
		if age > counts.OldestPendingAgeSeconds {
			counts.OldestPendingAgeSeconds = age
		}
	}
	return counts, nil
}
