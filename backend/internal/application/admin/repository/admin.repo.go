// Package repository is the persistence edge of the admin domain. It owns the
// three statements the bootstrap needs today; the delivery surface the panel adds
// later will grow here, never in a handler (golden rule: handler → service →
// repository).
package repository

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
)

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
}

type adminRepository struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewRepository builds the GORM-backed repository; Fx injects the handle.
func NewRepository(db *gorm.DB, logger *zap.Logger) Repository {
	return &adminRepository{db: db, logger: logger}
}

func (r *adminRepository) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&entity.Admin{}).Count(&count).Error; err != nil {
		r.logger.Error("count admins", zap.Error(err))
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return count, nil
}

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
