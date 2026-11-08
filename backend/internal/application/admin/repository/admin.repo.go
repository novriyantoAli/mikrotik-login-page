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
	"time"

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

// ErrAdminNotFound means the admin id a write named does not exist. It is a
// repository sentinel so the service answers 404 without matching on GORM.
var ErrAdminNotFound = errors.New("admin not found")

// LiveSession is one session that may still be used: not revoked, not expired,
// and belonging to an active admin. The admin row travels with it because the
// middleware needs the role and the audit trail needs the actor, and both must
// come from the same lookup the session did (ADR 0024 §2).
type LiveSession struct {
	Session entity.Session
	Admin   entity.Admin
}

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

	// AdminByEmail returns the row a login must compare against, or nil when the
	// address is unknown. A nil answer is what makes the login path uniform: the
	// service cannot tell "no such admin" from "wrong password" (ADR 0024 §4).
	AdminByEmail(ctx context.Context, email string) (*entity.Admin, error)
	// AdminByID returns one admin by id, or nil when it does not exist.
	AdminByID(ctx context.Context, id uint) (*entity.Admin, error)
	// ListAdmins returns every admin, active or not, in a stable order so the
	// panel's list does not reshuffle between two reads.
	ListAdmins(ctx context.Context) ([]entity.Admin, error)

	// ListAudit returns one page of the append-only trail, newest first, plus the
	// total number of matching rows. Filter values are repository-owned (see
	// AuditListParams), so the HTTP validation tags cannot change what persistence
	// queries on (golden rule 14).
	ListAudit(ctx context.Context, params AuditListParams) ([]entity.AuditEntry, int64, error)

	// CreateSession stores a freshly minted admin session. Only the token hash is
	// written; the raw token lives in the cookie and nowhere else (ADR 0024 §2).
	CreateSession(ctx context.Context, session *entity.Session) error
	// Session resolves a token hash into a live session and its admin. An expired,
	// revoked, unknown or inactive-admin session answers nil, never an error: the
	// middleware turns every one of those into the same 401.
	Session(ctx context.Context, tokenHash string, now time.Time) (*LiveSession, error)
	// RevokeSession marks one session revoked; an unknown hash is not an error, so
	// sign-out is idempotent.
	RevokeSession(ctx context.Context, tokenHash string, now time.Time) error
	// RevokeAllForAdmin revokes every live session of one admin. Password reset and
	// deactivation call it inside the same transaction as their change, so the
	// revocation is a server-side fact rather than a cookie the browser drops
	// (ADR 0024 §5).
	RevokeAllForAdmin(ctx context.Context, adminID uint, now time.Time) error

	// ChangePassword rewrites one admin's hash. It touches no session: changing
	// your own password is not revocation, resetting somebody else's is.
	ChangePassword(ctx context.Context, adminID uint, passwordHash string) error
	// ResetPassword rewrites the hash, revokes every session of that admin and
	// appends the trail row, all inside one transaction (ADR 0024 §5, ADR 0027 §2).
	// The target and the revoke instant are completed here, because only the
	// transaction knows when it commits.
	ResetPassword(ctx context.Context, adminID uint, passwordHash string, now time.Time, entry audit.Entry) error
	// SetActive changes one admin's is_active flag. Deactivating also revokes every
	// session and appends the trail row in the same transaction; reactivating
	// touches no session because there is none left to trust.
	SetActive(ctx context.Context, adminID uint, active bool, now time.Time, entry audit.Entry) error
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
	err := database.WithinTx(ctx, r.db, func(tx database.Tx) error {
		handle := tx.DB()
		if err := handle.Create(admin).Error; err != nil {
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
		return r.audit.Record(ctx, tx, recorded)
	})
	if err != nil {
		return fmt.Errorf("create admin %q: %w", admin.Email, err)
	}
	return nil
}

func (r *adminRepository) AdminByEmail(ctx context.Context, email string) (*entity.Admin, error) {
	var admin entity.Admin

	err := r.db.WithContext(ctx).Where("email = ?", email).First(&admin).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Unknown is not a failure: the login path answers it exactly like a wrong
		// password, so it must not be logged as one either (ADR 0024 §4).
		return nil, nil
	case err != nil:
		r.logger.Error("read admin by email", zap.String("email", email), zap.Error(err))
		return nil, fmt.Errorf("read admin by email %q: %w", email, err)
	}
	return &admin, nil
}

func (r *adminRepository) AdminByID(ctx context.Context, id uint) (*entity.Admin, error) {
	var admin entity.Admin

	err := r.db.WithContext(ctx).First(&admin, id).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		r.logger.Error("read admin by id", zap.Uint("admin_id", id), zap.Error(err))
		return nil, fmt.Errorf("read admin %d: %w", id, err)
	}
	return &admin, nil
}

func (r *adminRepository) ListAdmins(ctx context.Context) ([]entity.Admin, error) {
	var admins []entity.Admin

	if err := r.db.WithContext(ctx).Order("email ASC").Find(&admins).Error; err != nil {
		r.logger.Error("list admins", zap.Error(err))
		return nil, fmt.Errorf("list admins: %w", err)
	}
	return admins, nil
}

func (r *adminRepository) CreateSession(ctx context.Context, session *entity.Session) error {
	if err := r.db.WithContext(ctx).Create(session).Error; err != nil {
		r.logger.Error("create admin session", zap.Uint("admin_id", session.AdminID), zap.Error(err))
		return fmt.Errorf("create admin session: %w", err)
	}
	return nil
}

// Session is the only read the admin middleware trusts. Every disqualifying
// condition answers nil, so "revoked", "expired", "unknown" and "inactive admin"
// are one answer the transport renders as 401 (ADR 0024 §2).
func (r *adminRepository) Session(
	ctx context.Context,
	tokenHash string,
	now time.Time,
) (*LiveSession, error) {
	var session entity.Session

	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash, now).
		First(&session).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		r.logger.Error("read admin session", zap.Error(err))
		return nil, fmt.Errorf("read admin session: %w", err)
	}

	admin, err := r.AdminByID(ctx, session.AdminID)
	if err != nil {
		return nil, err
	}
	if admin == nil || !admin.IsActive {
		// Deactivation revokes sessions in its own transaction, so this is the
		// belt-and-braces check for any session that outlived it (ADR 0024 §5).
		return nil, nil
	}
	return &LiveSession{Session: session, Admin: *admin}, nil
}

func (r *adminRepository) RevokeSession(ctx context.Context, tokenHash string, now time.Time) error {
	err := r.db.WithContext(ctx).
		Model(&entity.Session{}).
		Where("token_hash = ? AND revoked_at IS NULL", tokenHash).
		Update("revoked_at", now).Error
	if err != nil {
		r.logger.Error("revoke admin session", zap.Error(err))
		return fmt.Errorf("revoke admin session: %w", err)
	}
	return nil
}

func (r *adminRepository) RevokeAllForAdmin(ctx context.Context, adminID uint, now time.Time) error {
	return r.revokeAll(r.db.WithContext(ctx), adminID, now)
}

// revokeAll is the one statement that turns an admin's sessions off, shared by the
// standalone method and the reset/deactivation transactions so the rule cannot
// drift between them.
func (r *adminRepository) revokeAll(handle *gorm.DB, adminID uint, now time.Time) error {
	err := handle.Model(&entity.Session{}).
		Where("admin_id = ? AND revoked_at IS NULL", adminID).
		Update("revoked_at", now).Error
	if err != nil {
		r.logger.Error("revoke admin sessions", zap.Uint("admin_id", adminID), zap.Error(err))
		return fmt.Errorf("revoke sessions for admin %d: %w", adminID, err)
	}
	return nil
}

func (r *adminRepository) ChangePassword(ctx context.Context, adminID uint, passwordHash string) error {
	result := r.db.WithContext(ctx).
		Model(&entity.Admin{}).
		Where("id = ?", adminID).
		Update("password_hash", passwordHash)
	if result.Error != nil {
		r.logger.Error("change admin password", zap.Uint("admin_id", adminID), zap.Error(result.Error))
		return fmt.Errorf("change password for admin %d: %w", adminID, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrAdminNotFound
	}
	return nil
}

// ResetPassword is ADR 0024 §5 and ADR 0027 §2 in one function: the new hash, the
// revocation of every session and the trail row commit or roll back together.
func (r *adminRepository) ResetPassword(
	ctx context.Context,
	adminID uint,
	passwordHash string,
	now time.Time,
	entry audit.Entry,
) error {
	return r.updateAdminAndRecord(ctx, adminID, map[string]any{"password_hash": passwordHash}, now, true, entry)
}

// SetActive flips is_active. Deactivation revokes sessions; reactivation does not
// create any, because a revoked cookie stays useless until the next login.
func (r *adminRepository) SetActive(
	ctx context.Context,
	adminID uint,
	active bool,
	now time.Time,
	entry audit.Entry,
) error {
	return r.updateAdminAndRecord(ctx, adminID, map[string]any{"is_active": active}, now, !active, entry)
}

// updateAdminAndRecord is the shape every audited admin write shares: one row
// update, an optional revocation of that admin's sessions, and one trail row, all
// in the caller's own transaction so no half of it can survive.
func (r *adminRepository) updateAdminAndRecord(
	ctx context.Context,
	adminID uint,
	changes map[string]any,
	now time.Time,
	revoke bool,
	entry audit.Entry,
) error {
	err := database.WithinTx(ctx, r.db, func(tx database.Tx) error {
		handle := tx.DB()
		result := handle.Model(&entity.Admin{}).Where("id = ?", adminID).Updates(changes)
		if result.Error != nil {
			r.logger.Error("update admin", zap.Uint("admin_id", adminID), zap.Error(result.Error))
			return fmt.Errorf("update admin %d: %w", adminID, result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrAdminNotFound
		}

		if revoke {
			if err := r.revokeAll(handle, adminID, now); err != nil {
				return err
			}
		}

		recorded := entry.WithTarget(audit.TargetAdmin, strconv.FormatUint(uint64(adminID), 10))
		return r.audit.Record(ctx, tx, recorded)
	})
	if err != nil {
		return fmt.Errorf("update admin %d: %w", adminID, err)
	}
	return nil
}
