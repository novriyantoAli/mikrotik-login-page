// Package service owns the SMS outbox rules. Enqueueing is separated from
// delivering on purpose: the API process records messages but must never need a
// modem, and the worker process delivers them (ADR 0010).
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/queue"
)

// Message is one SMS: a canonical Phone Number and the text to deliver.
type Message struct {
	PhoneNumber string
	Text        string
}

// Outbox is what the rest of the backend uses to send an SMS. It is the only
// surface other domains see; none of them touches the modem or the table.
type Outbox interface {
	// Enqueue records a message that must be delivered. It returns once the row
	// is durable, so a caller can treat "Enqueue returned nil" as "this message
	// will be retried until the modem takes it" (ADR 0010).
	Enqueue(ctx context.Context, message Message) error
	// EnqueueTx records the message inside the caller's transaction, so a
	// provisioning result and its PIN SMS commit together and "provisioned but
	// never queued" cannot happen (ADR 0010/0016).
	EnqueueTx(ctx context.Context, tx database.Tx, message Message) error
}

type outbox struct {
	repo   repository.Repository
	clock  clock.Clock
	waker  queue.Enqueuer
	logger *zap.Logger
}

// NewOutbox builds the enqueue-only outbox; Fx injects the repository, the
// frozen clock tests rely on and the Asynq wake-up enqueuer.
func NewOutbox(repo repository.Repository, clock clock.Clock, waker queue.Enqueuer, logger *zap.Logger) Outbox {
	return &outbox{repo: repo, clock: clock, waker: waker, logger: logger}
}

func (o *outbox) Enqueue(ctx context.Context, message Message) error {
	row, err := o.pending(message)
	if err != nil {
		return err
	}

	if err := o.repo.Enqueue(ctx, row); err != nil {
		return fmt.Errorf("enqueue sms: %w", err)
	}
	o.wake(ctx, row)
	return nil
}

func (o *outbox) EnqueueTx(ctx context.Context, tx database.Tx, message Message) error {
	row, err := o.pending(message)
	if err != nil {
		return err
	}

	if err := o.repo.EnqueueTx(ctx, tx, row); err != nil {
		return fmt.Errorf("enqueue sms: %w", err)
	}

	// The wake-up is not transactional: a task that arrives before the commit (or
	// after a rollback) only makes the drainer find nothing, and a lost wake-up
	// costs one sweep interval, never the message (ADR 0016).
	o.wake(ctx, row)
	return nil
}

// pending builds the durable row both enqueue paths write.
func (o *outbox) pending(message Message) (*entity.SMS, error) {
	if message.PhoneNumber == "" || message.Text == "" {
		return nil, errors.New("sms needs a Phone Number and a text")
	}

	now := o.clock.Now()
	return &entity.SMS{
		PhoneNumber:   message.PhoneNumber,
		Message:       message.Text,
		State:         entity.StatePending,
		NextAttemptAt: now,
	}, nil
}

// wake asks the worker for prompt delivery. A Redis hiccup only delays the
// message, so it never fails the caller (ADR 0010).
func (o *outbox) wake(ctx context.Context, row *entity.SMS) {
	if err := o.waker.Enqueue(ctx, sms.TaskDeliver); err != nil {
		o.logger.Warn("sms queued without wake-up", zap.Uint("sms_id", row.ID), zap.Error(err))
	}

	o.logger.Info("sms queued", zap.Uint("sms_id", row.ID))
}

// Settings are the delivery knobs the drainer needs, kept in a local struct so
// the domain never depends on the whole deployment configuration.
type Settings struct {
	// Lease is how long one claim owns a row.
	Lease time.Duration
	// MaxAttempts is the attempt budget before a row is left failed.
	MaxAttempts int
	// RetryBackoff is the base delay; it doubles per attempt.
	RetryBackoff time.Duration
}
