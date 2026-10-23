package service

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
)

// Deliverer sends queued messages. Only the worker process builds one, because
// only it is allowed to hold the modem (ADR 0010).
type Deliverer interface {
	// DeliverDue sends at most max due messages and reports how many it sent.
	// A modem failure is recorded on the row, not returned as an error, so one
	// stuck message never stops the drainer.
	DeliverDue(ctx context.Context, max int) (int, error)
}

type deliverer struct {
	repo     repository.Repository
	sender   sms.Sender
	clock    clock.Clock
	settings Settings
	logger   *zap.Logger
}

// NewDeliverer builds the drainer service; Fx injects the outbox repository, the
// modem port, the frozen clock and the delivery settings.
func NewDeliverer(
	repo repository.Repository,
	sender sms.Sender,
	clock clock.Clock,
	settings Settings,
	logger *zap.Logger,
) Deliverer {
	return &deliverer{repo: repo, sender: sender, clock: clock, settings: settings, logger: logger}
}

func (d *deliverer) DeliverDue(ctx context.Context, max int) (int, error) {
	delivered := 0

	for range max {
		row, err := d.repo.ClaimDue(ctx, d.clock.Now(), d.settings.Lease)
		if err != nil {
			return delivered, fmt.Errorf("claim due sms: %w", err)
		}
		if row == nil {
			return delivered, nil
		}

		if sendErr := d.sender.Send(ctx, row.PhoneNumber, row.Message); sendErr != nil {
			// One failure means the modem is unavailable; stop the batch instead
			// of hammering it, and let the row's backoff schedule the retry.
			d.recordFailure(ctx, row, sendErr)
			return delivered, nil
		}

		if err := d.repo.MarkSent(ctx, row.ID, d.clock.Now()); err != nil {
			return delivered, fmt.Errorf("mark sms sent: %w", err)
		}
		delivered++
		d.logger.Info("sms sent", zap.Uint("sms_id", row.ID), zap.Int("attempts", row.Attempts))
	}

	return delivered, nil
}

func (d *deliverer) recordFailure(ctx context.Context, row *entity.SMS, sendErr error) {
	var nextAttemptAt *time.Time
	if row.Attempts < d.settings.MaxAttempts {
		next := d.clock.Now().Add(d.backoff(row.Attempts))
		nextAttemptAt = &next
	}

	if err := d.repo.MarkFailed(ctx, row.ID, nextAttemptAt, sendErr.Error(), d.clock.Now()); err != nil {
		d.logger.Error("record sms failure", zap.Uint("sms_id", row.ID), zap.Error(err))
	}

	fields := []zap.Field{
		zap.Uint("sms_id", row.ID),
		zap.Int("attempts", row.Attempts),
		zap.Error(sendErr),
	}
	if nextAttemptAt == nil {
		d.logger.Error("sms left failed after its last attempt", fields...)
		return
	}
	d.logger.Warn("sms delivery failed; retry scheduled",
		append(fields, zap.Time("retry_at", *nextAttemptAt))...)
}

// backoff doubles the configured delay per attempt, so a modem that is off for a
// while is not retried every few seconds forever.
func (d *deliverer) backoff(attempts int) time.Duration {
	delay := d.settings.RetryBackoff
	for i := 1; i < attempts && delay < 24*time.Hour; i++ {
		delay *= 2
	}
	return delay
}
