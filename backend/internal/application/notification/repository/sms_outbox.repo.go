// Package repository is the persistence edge of the SMS outbox. It owns every
// statement that moves a row between states, so the drainer only has to make
// business decisions (ADR 0010).
package repository

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// Repository queues and claims SMS rows.
type Repository interface {
	// Enqueue writes one pending row. The caller is expected to be inside no
	// transaction of its own yet; the row is the durable record of the message.
	Enqueue(ctx context.Context, message *entity.SMS) error
	// EnqueueTx writes one pending row inside the caller's transaction, so a
	// message and the change that produced it commit together (ADR 0010/0016).
	EnqueueTx(ctx context.Context, tx database.Tx, message *entity.SMS) error
	// ClaimDue returns the next row that is due, marks it sending and gives it a
	// lease, or returns nil when nothing is due. Rows whose lease expired are
	// returned to pending first, so a crashed drainer never strands a message.
	ClaimDue(ctx context.Context, now time.Time, lease time.Duration) (*entity.SMS, error)
	// MarkSent closes a claimed row successfully.
	MarkSent(ctx context.Context, id uint, sentAt time.Time) error
	// MarkFailed either reschedules the row (nextAttemptAt set) or closes it as
	// failed (nextAttemptAt nil). The policy that chooses between the two lives
	// in the service, including the timestamp, so the repository never reads the
	// wall clock on its own.
	MarkFailed(ctx context.Context, id uint, nextAttemptAt *time.Time, cause string, now time.Time) error
}

type smsRepository struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewRepository builds the GORM-backed outbox; Fx injects the handle.
func NewRepository(db *gorm.DB, logger *zap.Logger) Repository {
	return &smsRepository{db: db, logger: logger}
}

func (r *smsRepository) Enqueue(ctx context.Context, message *entity.SMS) error {
	if err := r.db.WithContext(ctx).Create(message).Error; err != nil {
		r.logger.Error("enqueue sms", zap.Error(err))
		return fmt.Errorf("enqueue sms: %w", err)
	}
	return nil
}

func (r *smsRepository) EnqueueTx(ctx context.Context, tx database.Tx, message *entity.SMS) error {
	if err := tx.DB().WithContext(ctx).Create(message).Error; err != nil {
		r.logger.Error("enqueue sms in the caller's transaction", zap.Error(err))
		return fmt.Errorf("enqueue sms: %w", err)
	}
	return nil
}

func (r *smsRepository) ClaimDue(ctx context.Context, now time.Time, lease time.Duration) (*entity.SMS, error) {
	var claimed *entity.SMS

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A drainer that died mid-send leaves a sending row behind; once its
		// lease is over the row is claimable again (at-least-once, ADR 0010).
		if err := tx.Model(&entity.SMS{}).
			Where("state = ? AND lease_until IS NOT NULL AND lease_until < ?", entity.StateSending, now).
			Updates(map[string]any{
				"state":       entity.StatePending,
				"lease_until": nil,
				"updated_at":  now,
			}).Error; err != nil {
			return fmt.Errorf("release expired leases: %w", err)
		}

		var next entity.SMS
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("state = ? AND next_attempt_at <= ?", entity.StatePending, now).
			Order("id ASC").
			First(&next).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return fmt.Errorf("claim due sms: %w", err)
		}

		leaseUntil := now.Add(lease)
		updates := map[string]any{
			"state":       entity.StateSending,
			"attempts":    next.Attempts + 1,
			"lease_until": leaseUntil,
			"updated_at":  now,
		}
		if err := tx.Model(&entity.SMS{}).Where("id = ?", next.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("lease sms %d: %w", next.ID, err)
		}

		next.State = entity.StateSending
		next.Attempts++
		next.LeaseUntil = &leaseUntil
		claimed = &next
		return nil
	})
	if err != nil {
		r.logger.Error("claim due sms", zap.Error(err))
		return nil, err
	}
	return claimed, nil
}

func (r *smsRepository) MarkSent(ctx context.Context, id uint, sentAt time.Time) error {
	updates := map[string]any{
		"state":       entity.StateSent,
		"lease_until": nil,
		"sent_at":     sentAt,
		"last_error":  "",
		// The body carries a PIN or a Magic Link token. While the row is pending the
		// modem needs it; once the message is out there is no reason for Postgres to
		// keep a clear-text credential (ADR 0016).
		"message":    "",
		"updated_at": sentAt,
	}
	if err := r.db.WithContext(ctx).Model(&entity.SMS{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		r.logger.Error("mark sms sent", zap.Uint("sms_id", id), zap.Error(err))
		return fmt.Errorf("mark sms %d sent: %w", id, err)
	}
	return nil
}

func (r *smsRepository) MarkFailed(
	ctx context.Context,
	id uint,
	nextAttemptAt *time.Time,
	cause string,
	now time.Time,
) error {
	state := entity.StateFailed
	updates := map[string]any{
		"lease_until": nil,
		"last_error":  cause,
	}
	if nextAttemptAt != nil {
		state = entity.StatePending
		updates["next_attempt_at"] = *nextAttemptAt
	} else {
		// The last attempt failed, so there is no retry to feed: drop the clear-text
		// credential instead of keeping it forever (ADR 0016).
		updates["message"] = ""
	}
	updates["state"] = state
	updates["updated_at"] = now

	if err := r.db.WithContext(ctx).Model(&entity.SMS{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		r.logger.Error("mark sms failed", zap.Uint("sms_id", id), zap.Error(err))
		return fmt.Errorf("mark sms %d failed: %w", id, err)
	}
	return nil
}
