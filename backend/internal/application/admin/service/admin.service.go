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

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
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

	// ErrEmailTaken means another admin already holds the address; the transport
	// answers 409, never both "taken" and "invalid".
	ErrEmailTaken = errors.New("admin email is already in use")
	// ErrEmailInvalid means the submitted address is not an address; the bootstrap
	// has its own sentinel because its message must name ADMIN_EMAIL.
	ErrEmailInvalid = errors.New("admin email is not an email address")
	// ErrRoleInvalid means the submitted role is not one of the three the
	// permission matrix knows (ADR 0024 §3).
	ErrRoleInvalid = errors.New("admin role is not one of owner, support, auditor")
	// ErrPasswordWeak means the submitted password breaks the panel's own rule.
	ErrPasswordWeak = errors.New("password does not satisfy the admin password rule")
	// ErrActorRequired means a write arrived without the admin who performed it.
	// Refusing is the point: an audited action with no actor is a trail row that
	// cannot answer the question it exists for (ADR 0027 §3).
	ErrActorRequired = errors.New("an audited action needs its actor")
	// ErrAdminNotFound means the admin a management write named does not exist; the
	// transport answers 404.
	ErrAdminNotFound = errors.New("admin not found")
	// ErrRequestInvalid means a write body is missing a value the service needs,
	// even when a transport's binding tag would have refused it first. The rules are
	// enforced here too, because a rule only the form knows is a rule the next
	// transport forgets.
	ErrRequestInvalid = errors.New("admin request is invalid")
)

// Service is the admin domain's public surface.
type Service interface {
	// BootstrapOwner inserts the first owner when the admins table is empty and
	// reports how many rows it added. Once any admin exists the settings are
	// ignored completely: no password is reset, and an incomplete or invalid pair
	// is not an error either.
	BootstrapOwner(ctx context.Context) (int64, error)
	// CreateAdmin adds one panel actor and leaves exactly one trail row for it,
	// in the same database transaction (ADR 0027 §2). The actor is the owner who
	// performed the write, never the admin being created.
	CreateAdmin(ctx context.Context, actor audit.Actor, request *dto.CreateAdminRequest) (*dto.AdminResponse, error)
	// ListAdmins returns every admin, so an owner can see the team they manage.
	ListAdmins(ctx context.Context) ([]dto.AdminResponse, error)
	// ListAudit returns one page of the append-only trail for an owner or an
	// auditor (ADR 0027 §3). It is a read: no method here can rewrite a row.
	ListAudit(ctx context.Context, filter *dto.AdminAuditFilter) (*dto.AdminAuditPage, error)
	// ResetPassword sets a colleague's password and revokes every session that
	// admin has, in the same transaction as the trail row (ADR 0024 §5).
	ResetPassword(ctx context.Context, actor audit.Actor, adminID uint, request *dto.ResetPasswordRequest) error
	// SetActive enables or disables one admin. Deactivating revokes every session
	// of that admin at once, so the old cookie stops working immediately.
	SetActive(ctx context.Context, actor audit.Actor, adminID uint, request *dto.SetActiveRequest) error
}

type service struct {
	repo     repository.Repository
	settings BootstrapSettings
	clock    clock.Clock
	logger   *zap.Logger
}

// NewService builds the admin service; Fx injects the repository, the
// deployment's bootstrap pair and the clock the revocations are stamped with.
func NewService(
	repo repository.Repository,
	settings BootstrapSettings,
	clock clock.Clock,
	logger *zap.Logger,
) Service {
	return &service{repo: repo, settings: settings, clock: clock, logger: logger}
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

	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	return &entity.Admin{
		Email:        canonical,
		PasswordHash: hash,
		Role:         entity.RoleOwner,
		IsActive:     true,
	}, nil
}

// CreateAdmin adds one panel actor and leaves one trail row for it. The rules are
// enforced here rather than only in the form: the panel is one caller of the
// service, and a rule the next transport forgets is a rule the audit row would be
// the only witness of.
func (s *service) CreateAdmin(
	ctx context.Context,
	actor audit.Actor,
	request *dto.CreateAdminRequest,
) (*dto.AdminResponse, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}

	role, err := parseRole(request.Role)
	if err != nil {
		return nil, err
	}

	email, err := canonicalEmail(request.Email)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmailInvalid, err)
	}
	if err := checkPassword(email, request.Password); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPasswordWeak, err)
	}

	hash, err := hashPassword(request.Password)
	if err != nil {
		s.logger.Error("hash admin password", zap.Error(err))
		return nil, err
	}

	admin := &entity.Admin{
		Email:        email,
		PasswordHash: hash,
		Role:         role,
		IsActive:     true,
	}
	// The payload is the after state of the fields an owner chose, never the
	// credential: a trail that holds a hash is a trail that leaks one (ADR 0027 §4).
	entry := audit.Entry{
		Actor:      actor,
		Action:     audit.ActionAdminCreate,
		TargetType: audit.TargetAdmin,
		Payload:    map[string]any{"email": email, "role": string(role), "is_active": true},
	}

	if err := s.repo.CreateAdmin(ctx, admin, entry); err != nil {
		if errors.Is(err, repository.ErrDuplicateEmail) {
			return nil, fmt.Errorf("%w: %s", ErrEmailTaken, email)
		}
		// The repository already logged the statement it saw fail; logging it again
		// here would report one failure twice.
		return nil, err
	}

	s.logger.Info("admin created",
		zap.Uint("admin_id", admin.ID),
		zap.String("email", admin.Email),
		zap.String("role", string(admin.Role)),
		zap.String("actor", actor.Email),
	)
	return adminToResponse(*admin), nil
}

// parseRole accepts only the three roles the permission matrix knows (ADR 0024
// §3); anything else would become a fourth role the middleware does not know.
func parseRole(raw string) (entity.Role, error) {
	role := entity.Role(strings.ToLower(strings.TrimSpace(raw)))
	switch role {
	case entity.RoleOwner, entity.RoleSupport, entity.RoleAuditor:
		return role, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrRoleInvalid, raw)
	}
}

// adminToResponse maps the entity onto the panel's shape.
func adminToResponse(admin entity.Admin) *dto.AdminResponse {
	return &dto.AdminResponse{
		ID:        admin.ID,
		Email:     admin.Email,
		Role:      string(admin.Role),
		IsActive:  admin.IsActive,
		CreatedAt: admin.CreatedAt,
	}
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

// ListAdmins returns every admin, active or not. It has no actor argument because
// reading the team is not an audited act: the middleware already refused the roles
// that may not ask (ADR 0024 §3).
func (s *service) ListAdmins(ctx context.Context) ([]dto.AdminResponse, error) {
	admins, err := s.repo.ListAdmins(ctx)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}

	responses := make([]dto.AdminResponse, 0, len(admins))
	for _, admin := range admins {
		responses = append(responses, *adminToResponse(admin))
	}
	return responses, nil
}

// ResetPassword sets a colleague's password. The revocation of that admin's
// sessions is part of the same repository transaction as the new hash and the
// trail row: a reset that leaves a live session behind is a reset that did not
// happen (ADR 0024 §5).
func (s *service) ResetPassword(
	ctx context.Context,
	actor audit.Actor,
	adminID uint,
	request *dto.ResetPasswordRequest,
) error {
	if err := requireActor(actor); err != nil {
		return err
	}

	target, err := requireAdmin(ctx, s.repo, adminID)
	if err != nil {
		return err
	}
	if err := checkPassword(target.Email, request.Password); err != nil {
		return fmt.Errorf("%w: %v", ErrPasswordWeak, err)
	}

	hash, err := hashPassword(request.Password)
	if err != nil {
		s.logger.Error("hash admin password", zap.Error(err))
		return err
	}

	entry := audit.Entry{
		Actor:   actor,
		Action:  audit.ActionAdminPasswordReset,
		Payload: map[string]any{"email": target.Email},
	}
	if err := s.repo.ResetPassword(ctx, adminID, hash, s.clock.Now(), entry); err != nil {
		if errors.Is(err, repository.ErrAdminNotFound) {
			return fmt.Errorf("%w: %d", ErrAdminNotFound, adminID)
		}
		return err
	}

	s.logger.Info("admin password reset",
		zap.Uint("admin_id", adminID),
		zap.String("email", target.Email),
		zap.String("actor", actor.Email),
	)
	return nil
}

// SetActive enables or disables one admin. Deactivating answers with a revocation
// of every session of that admin, in the same transaction (ADR 0024 §5).
func (s *service) SetActive(
	ctx context.Context,
	actor audit.Actor,
	adminID uint,
	request *dto.SetActiveRequest,
) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	if request == nil || request.Active == nil {
		// A second transport may not validate the pointer a first one did, and a nil
		// dereference is a 500 where the caller deserved a 422.
		return fmt.Errorf("%w: active is required", ErrRequestInvalid)
	}

	target, err := requireAdmin(ctx, s.repo, adminID)
	if err != nil {
		return err
	}

	action := audit.ActionAdminUpdate
	if !*request.Active {
		action = audit.ActionAdminDeactivate
	}
	entry := audit.Entry{
		Actor:   actor,
		Action:  action,
		Payload: map[string]any{"email": target.Email, "is_active": *request.Active},
	}
	if err := s.repo.SetActive(ctx, adminID, *request.Active, s.clock.Now(), entry); err != nil {
		if errors.Is(err, repository.ErrAdminNotFound) {
			return fmt.Errorf("%w: %d", ErrAdminNotFound, adminID)
		}
		return err
	}

	s.logger.Info("admin active flag changed",
		zap.Uint("admin_id", adminID),
		zap.String("email", target.Email),
		zap.Bool("active", *request.Active),
		zap.String("actor", actor.Email),
	)
	return nil
}

// hashPassword is the one place the panel's work factor is applied, so a login, a
// reset and a self-service change can never drift to different costs (ADR 0024 §1).
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	if err != nil {
		return "", fmt.Errorf("hash admin password: %w", err)
	}
	return string(hash), nil
}

// requireActor is the one gate every audited write shares: no actor, no trail row,
// no write (ADR 0027 §3).
func requireActor(actor audit.Actor) error {
	if actor.ID == 0 || strings.TrimSpace(actor.Email) == "" {
		return ErrActorRequired
	}
	return nil
}

// requireAdmin resolves the row a management write targets, so an unknown id is a
// 404 before anything is written rather than a silent no-op. It is a free function
// because both services in this package need the same gate.
func requireAdmin(ctx context.Context, repo repository.Repository, adminID uint) (*entity.Admin, error) {
	admin, err := repo.AdminByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if admin == nil {
		return nil, fmt.Errorf("%w: %d", ErrAdminNotFound, adminID)
	}
	return admin, nil
}
