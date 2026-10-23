// Package repository is the persistence edge of the Transaction domain. Every
// state change is a single guarded UPDATE (`WHERE state = <expected>`), the same
// pattern the Magic Link (used_at IS NULL) and the SMS outbox use: two concurrent
// callers cannot both win, and the loser learns it changed nothing.
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
)

// ErrNotFound means no Transaction matches the lookup (including one that
// belongs to another Phone Number: the service reports both as not found).
var ErrNotFound = errors.New("transaction not found")

// Repository persists Transactions.
type Repository interface {
	// Create stores a new Transaction. When another pending Transaction already
	// exists for the same Phone Number and Package, the partial unique index
	// rejects this insert; the caller resolves that into the existing row.
	Create(ctx context.Context, transaction *entity.Transaction) error
	// GetByID returns one Transaction or ErrNotFound.
	GetByID(ctx context.Context, id uint) (*entity.Transaction, error)
	// GetByReference returns one Transaction by this system's reference or
	// ErrNotFound. Callbacks look transactions up this way.
	GetByReference(ctx context.Context, reference string) (*entity.Transaction, error)
	// Pending returns the live pending Transaction for one Phone Number and
	// Package, or nil when there is none. This is the idempotency read of
	// checkout: a repeat "Bayar" gets the same Transaction back.
	Pending(ctx context.Context, phoneNumber string, packageID uint, now time.Time) (*entity.Transaction, error)
	// Transition moves a Transaction from one state to another only when it is
	// still in the expected state, and reports whether it did. A repeated
	// callback loses the race here instead of transitioning twice.
	Transition(ctx context.Context, id uint, from, to entity.State, at time.Time) (bool, error)
	// ExpireOverdue moves every pending Transaction whose deadline has passed to
	// expired and reports how many it changed. The clock is the caller's, so the
	// rule stays testable.
	ExpireOverdue(ctx context.Context, now time.Time) (int64, error)
}

type transactionRepository struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewRepository builds the GORM-backed repository; Fx injects the handle.
func NewRepository(db *gorm.DB, logger *zap.Logger) Repository {
	return &transactionRepository{db: db, logger: logger}
}

func (r *transactionRepository) Create(ctx context.Context, transaction *entity.Transaction) error {
	if err := r.db.WithContext(ctx).Create(transaction).Error; err != nil {
		// The caller decides whether the failure is the "one pending per session"
		// guard; logging it as an error here would report normal races as faults.
		return fmt.Errorf("create transaction: %w", err)
	}
	return nil
}

func (r *transactionRepository) GetByID(ctx context.Context, id uint) (*entity.Transaction, error) {
	var row entity.Transaction
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		r.logger.Error("read transaction", zap.Uint("transaction_id", id), zap.Error(err))
		return nil, fmt.Errorf("read transaction %d: %w", id, err)
	}
	return &row, nil
}

func (r *transactionRepository) GetByReference(
	ctx context.Context,
	reference string,
) (*entity.Transaction, error) {
	var row entity.Transaction
	if err := r.db.WithContext(ctx).Where("reference = ?", reference).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		r.logger.Error("read transaction by reference", zap.String("reference", reference), zap.Error(err))
		return nil, fmt.Errorf("read transaction %s: %w", reference, err)
	}
	return &row, nil
}

func (r *transactionRepository) Pending(
	ctx context.Context,
	phoneNumber string,
	packageID uint,
	now time.Time,
) (*entity.Transaction, error) {
	var row entity.Transaction

	err := r.db.WithContext(ctx).
		Where("phone_number = ? AND package_id = ? AND state = ? AND expires_at > ?",
			phoneNumber, packageID, entity.StatePending, now).
		Order("id DESC").
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		r.logger.Error("read pending transaction", zap.Error(err))
		return nil, fmt.Errorf("read pending transaction: %w", err)
	}
	return &row, nil
}

func (r *transactionRepository) Transition(
	ctx context.Context,
	id uint,
	from, to entity.State,
	at time.Time,
) (bool, error) {
	updates := map[string]any{
		"state":      to,
		"updated_at": at,
	}
	if to == entity.StatePaid {
		updates["paid_at"] = at
	}

	result := r.db.WithContext(ctx).
		Model(&entity.Transaction{}).
		Where("id = ? AND state = ?", id, from).
		Updates(updates)
	if result.Error != nil {
		r.logger.Error("transition transaction",
			zap.Uint("transaction_id", id),
			zap.String("from", string(from)),
			zap.String("to", string(to)),
			zap.Error(result.Error),
		)
		return false, fmt.Errorf("transition transaction %d to %s: %w", id, to, result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *transactionRepository) ExpireOverdue(ctx context.Context, now time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&entity.Transaction{}).
		Where("state = ? AND expires_at <= ?", entity.StatePending, now).
		Updates(map[string]any{
			"state":      entity.StateExpired,
			"updated_at": now,
		})
	if result.Error != nil {
		r.logger.Error("expire overdue transactions", zap.Error(result.Error))
		return 0, fmt.Errorf("expire overdue transactions: %w", result.Error)
	}
	return result.RowsAffected, nil
}
