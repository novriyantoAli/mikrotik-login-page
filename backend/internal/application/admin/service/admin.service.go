// Package service owns the admin domain's rules. Its only job today is the
// first-owner bootstrap the deployment runs before the API starts: the panel
// must have a way in after a fresh deploy, and a deploy must never reset a
// password that already exists (ADR 0024 §6).
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
)

// PasswordCost is the bcrypt cost every admin password is hashed with (ADR
// 0024 §1). It is exported so the login path added later hashes and compares
// with the same work factor instead of inventing a second one.
const PasswordCost = 12

// MinPasswordLength is the only composition rule: length, no character-class
// matrix. Twelve characters beats a punctuation requirement nobody remembers
// (ADR 0024 §1).
const MinPasswordLength = 12

// maxPasswordBytes is bcrypt's input limit. Checked here so an over-long
// bootstrap password is refused with a message that names ADMIN_PASSWORD instead
// of surfacing from inside the hasher.
const maxPasswordBytes = 72

// BootstrapSettings are the two values `migrate` reads from the environment to
// create the first owner. They are empty in a steady-state deployment, where the
// admins table already has rows.
type BootstrapSettings struct {
	Email    string
	Password string
}

// Errors the migration step reports. They are sentinels so a caller can tell a
// refused seed from a broken database, while the message stays the thing an
// operator reads at 3am: it names the variable to set.
var (
	// ErrBootstrapCredentialsMissing means the table is empty and the deployment
	// gave no pair to create an owner from. The command must fail instead of
	// leaving a panel with no door.
	ErrBootstrapCredentialsMissing = errors.New("no admin exists and the bootstrap credentials are missing")
	// ErrBootstrapEmailInvalid means ADMIN_EMAIL is not an address.
	ErrBootstrapEmailInvalid = errors.New("ADMIN_EMAIL is not an email address")
	// ErrBootstrapPasswordWeak means ADMIN_PASSWORD breaks the panel's own rule.
	ErrBootstrapPasswordWeak = errors.New("ADMIN_PASSWORD does not satisfy the admin password rule")
)

// Service is the admin domain's public surface.
type Service interface {
	// BootstrapOwner inserts the first owner when the admins table is empty and
	// reports how many rows it added. Once any admin exists the settings are
	// ignored completely: no password is reset, and an incomplete or invalid pair
	// is not an error either.
	BootstrapOwner(ctx context.Context) (int64, error)
}

type service struct {
	repo     repository.Repository
	settings BootstrapSettings
	logger   *zap.Logger
}

// NewService builds the admin service; Fx injects the repository and the
// deployment's bootstrap pair.
func NewService(repo repository.Repository, settings BootstrapSettings, logger *zap.Logger) Service {
	return &service{repo: repo, settings: settings, logger: logger}
}

func (s *service) BootstrapOwner(ctx context.Context) (int64, error) {
	existing, err := s.repo.CountAdmins(ctx)
	if err != nil {
		return 0, err
	}
	if existing > 0 {
		// The environment is ignored entirely, not merely unused: a deploy that
		// still carries a bootstrap password must never touch an existing one.
		s.logger.Info("admin seed skipped, admins already exist", zap.Int64("admins", existing))
		return 0, nil
	}

	owner, err := s.ownerFromSettings()
	if err != nil {
		return 0, err
	}

	inserted, err := s.repo.InsertOwner(ctx, owner)
	if err != nil {
		return 0, fmt.Errorf("insert first owner: %w", err)
	}
	if inserted > 0 {
		s.logger.Info("first admin seeded", zap.String("email", owner.Email), zap.String("role", string(owner.Role)))
	}
	return inserted, nil
}

// ownerFromSettings turns the configured pair into the row to insert, validating
// it the way the panel's own rules would.
func (s *service) ownerFromSettings() (*entity.Admin, error) {
	email, password := s.settings.Email, s.settings.Password
	if strings.TrimSpace(email) == "" && password == "" {
		return nil, fmt.Errorf("%w: set ADMIN_EMAIL and ADMIN_PASSWORD", ErrBootstrapCredentialsMissing)
	}
	if strings.TrimSpace(email) == "" {
		return nil, fmt.Errorf("%w: ADMIN_EMAIL is empty", ErrBootstrapCredentialsMissing)
	}
	if password == "" {
		return nil, fmt.Errorf("%w: ADMIN_PASSWORD is empty", ErrBootstrapCredentialsMissing)
	}

	canonical, err := canonicalEmail(email)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBootstrapEmailInvalid, err)
	}
	if err := checkPassword(canonical, password); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBootstrapPasswordWeak, err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	if err != nil {
		return nil, fmt.Errorf("hash admin password: %w", err)
	}

	return &entity.Admin{
		Email:        canonical,
		PasswordHash: string(hash),
		Role:         entity.RoleOwner,
		IsActive:     true,
	}, nil
}

// canonicalEmail trims and lowercases an address, then rejects anything that
// cannot be one. Storing the canonical form is what makes the unique index an
// identity: the seed and a later login must agree on the spelling.
func canonicalEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" || domain == "" || strings.Contains(domain, "@") ||
		strings.ContainsAny(email, " \t\r\n") {
		return "", fmt.Errorf("%q is not an email address", raw)
	}
	return email, nil
}

// checkPassword applies the panel's password rule: length, and never the email
// itself. The upper bound is bcrypt's own: a longer passphrase would fail deep
// inside the hasher, where the message names no variable an operator could fix.
func checkPassword(email, password string) error {
	if len([]rune(password)) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	if len([]byte(password)) > maxPasswordBytes {
		return fmt.Errorf("password must be at most %d bytes", maxPasswordBytes)
	}
	if strings.EqualFold(strings.TrimSpace(password), email) {
		return errors.New("password must not be the admin's email")
	}
	return nil
}
