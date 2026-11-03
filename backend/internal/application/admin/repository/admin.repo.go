// Package repository is the persistence edge of the admin domain. It owns the
// three statements the bootstrap needs today; the delivery surface the panel adds
// later will grow here, never in a handler (golden rule: handler → service →
// repository).
package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// ErrDuplicateEmail means another admin already holds that address. The unique
// index is the authority, so the answer survives two owners creating the same
// colleague at once; the service turns it into a 409.
var ErrDuplicateEmail = errors.New("admin email already exists")

// Repository persists admin identities, their sessions and their audit trail.
type Repository interface {
	// CountAdmins reports how many admins exist. The bootstrap uses it to decide
	// whether the deployment still has no way in; a non-zero answer means the
	// environment is ignored entirely (ADR 0024 §6).
	CountAdmins(ctx context.Context) (int64, error)
	// InsertOwner stores one admin only if its email is not in the table yet and
	// reports how many rows it added. ON CONFLICT DO NOTHING is what makes a
	// deploy idempotent and what makes it impossible for a seed to reset a
	// password: an existing row is never rewritten.
	InsertOwner(ctx context.Context, admin *entity.Admin) (int64, error)
	// CreateAdmin stores one admin and appends its trail row inside the same
	// transaction, so a successful create without its audit row is not
	// representable and a refused one leaves nothing behind (ADR 0027 §2). The
	// new id completes entry's target; a taken address is ErrDuplicateEmail.
	CreateAdmin(ctx context.Context, admin *entity.Admin, entry audit.Entry) error
}

type adminRepository struct {
	db     *gorm.DB
	audit  *AuditRecorder
	logger *zap.Logger
}

// NewRepository builds the GORM-backed repository; Fx injects the handle and the
// audit adapter the domain's own writes record through.
func NewRepository(db *gorm.DB, recorder *AuditRecorder, logger *zap.Logger) Repository {
	return &adminRepository{db: db, audit: recorder, logger: logger}
}

func (r *adminRepository) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&entity.Admin{}).Count(&count).Error; err != nil {
		r.logger.Error("count admins", zap.Error(err))
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return count, nil
}

// InsertOwner seeds the first owner and is the one admin write with no trail row:
// nothing and nobody is signed in yet, ADR 0027 §3 names no bootstrap action, and a
// row attributed to an actor that does not exist would answer no question. Every
// write a panel actor performs goes through CreateAdmin instead.
func (r *adminRepository) InsertOwner(ctx context.Context, admin *entity.Admin) (int64, error) {
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "email"}}, DoNothing: true}).
		Create(admin)
	if result.Error != nil {
		r.logger.Error("insert admin", zap.String("email", admin.Email), zap.Error(result.Error))
		return 0, fmt.Errorf("insert admin %q: %w", admin.Email, result.Error)
	}
	return result.RowsAffected, nil
}

// CreateAdmin writes the identity and its trail row through one transaction: the
// insert and the audit entry commit together or not at all (ADR 0027 §2).
func (r *adminRepository) CreateAdmin(ctx context.Context, admin *entity.Admin, entry audit.Entry) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(admin).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				// Not logged as a failure: two owners racing for one address is a
				// conflict the service answers, not a broken database.
				return ErrDuplicateEmail
			}
			r.logger.Error("insert admin", zap.String("email", admin.Email), zap.Error(err))
			return fmt.Errorf("insert admin %q: %w", admin.Email, err)
		}

		// The row's identity only exists now, so the fact is completed here rather
		// than in the service that did not know it yet.
		recorded := entry.WithTarget(audit.TargetAdmin, strconv.FormatUint(uint64(admin.ID), 10))
		return r.audit.Record(ctx, database.NewTx(tx), recorded)
	})
	if err != nil {
		return fmt.Errorf("create admin %q: %w", admin.Email, err)
	}
	return nil
}
