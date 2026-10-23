// Package repository is the persistence edge of the auth domain. The two
// statements that matter — consume a Magic Link and look up a session — are
// single atomic queries, so two taps on the same link or a revoked cookie can
// never both succeed (ADR 0011).
package repository

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/entity"
)

// NewMagicLink describes the row to write for one issued link. It is owned by
// this layer: the service maps its own inputs onto it, never the HTTP dto.
type NewMagicLink struct {
	TokenHash   string
	PhoneNumber string
	Purpose     entity.Purpose
	PackageID   *uint
	ReturnURL   string
	ExpiresAt   time.Time
}

// Repository persists Magic Link tokens and sessions.
type Repository interface {
	// CreateMagicLink stores one issued link.
	CreateMagicLink(ctx context.Context, link NewMagicLink) error
	// ConsumeMagicLink marks the token used and returns it, or returns nil when
	// the token is unknown, already used or expired. The update is the guard, so
	// two concurrent redemptions cannot both win.
	ConsumeMagicLink(ctx context.Context, tokenHash string, now time.Time) (*entity.MagicLinkToken, error)
	// CreateSession stores a freshly minted session.
	CreateSession(ctx context.Context, session *entity.Session) error
	// Session looks up a live session: not expired and not revoked.
	Session(ctx context.Context, tokenHash string, now time.Time) (*entity.Session, error)
	// RevokeSession marks a session revoked; an unknown hash is not an error, so
	// sign-out is idempotent.
	RevokeSession(ctx context.Context, tokenHash string, now time.Time) error
}

type authRepository struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewRepository builds the GORM-backed repository; Fx injects the handle.
func NewRepository(db *gorm.DB, logger *zap.Logger) Repository {
	return &authRepository{db: db, logger: logger}
}

func (r *authRepository) CreateMagicLink(ctx context.Context, link NewMagicLink) error {
	row := &entity.MagicLinkToken{
		TokenHash:   link.TokenHash,
		PhoneNumber: link.PhoneNumber,
		Purpose:     link.Purpose,
		PackageID:   link.PackageID,
		ReturnURL:   link.ReturnURL,
		ExpiresAt:   link.ExpiresAt,
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		r.logger.Error("create magic link", zap.Error(err))
		return fmt.Errorf("create magic link: %w", err)
	}
	return nil
}

func (r *authRepository) ConsumeMagicLink(
	ctx context.Context,
	tokenHash string,
	now time.Time,
) (*entity.MagicLinkToken, error) {
	var token entity.MagicLinkToken

	result := r.db.WithContext(ctx).
		Model(&token).
		Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", tokenHash, now).
		Update("used_at", now)
	if result.Error != nil {
		r.logger.Error("consume magic link", zap.Error(result.Error))
		return nil, fmt.Errorf("consume magic link: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		// Unknown, already used or expired: the caller answers one uniform
		// message for all three (ADR 0011).
		return nil, nil
	}

	if err := r.db.WithContext(ctx).
		Where("token_hash = ?", tokenHash).
		First(&token).Error; err != nil {
		r.logger.Error("read consumed magic link", zap.Error(err))
		return nil, fmt.Errorf("read consumed magic link: %w", err)
	}
	return &token, nil
}

func (r *authRepository) CreateSession(ctx context.Context, session *entity.Session) error {
	if err := r.db.WithContext(ctx).Create(session).Error; err != nil {
		r.logger.Error("create session", zap.Error(err))
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *authRepository) Session(
	ctx context.Context,
	tokenHash string,
	now time.Time,
) (*entity.Session, error) {
	var session entity.Session

	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash, now).
		First(&session).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		r.logger.Error("read session", zap.Error(err))
		return nil, fmt.Errorf("read session: %w", err)
	}
	return &session, nil
}

func (r *authRepository) RevokeSession(ctx context.Context, tokenHash string, now time.Time) error {
	err := r.db.WithContext(ctx).
		Model(&entity.Session{}).
		Where("token_hash = ? AND revoked_at IS NULL", tokenHash).
		Update("revoked_at", now).Error
	if err != nil {
		r.logger.Error("revoke session", zap.Error(err))
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}
