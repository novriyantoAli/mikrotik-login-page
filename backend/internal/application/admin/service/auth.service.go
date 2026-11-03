package service

// This file owns the panel's sign-in surface: a password login with uniform
// failures, two rate limits, and a session table that is not the buyer's
// (ADR 0024 §2, §4). Nothing here knows about cookies or HTTP.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/ratelimit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/securetoken"
)

// Errors the auth surface reports. They are sentinels, not message text, so a
// transport never matches on strings.
var (
	// ErrCredentialsInvalid is the single answer for an unknown email, a wrong
	// password and a deactivated admin. One value is what keeps the endpoint from
	// enumerating admins (ADR 0024 §4).
	ErrCredentialsInvalid = errors.New("admin credentials are not valid")
	// ErrCurrentPasswordInvalid means the caller proved a session but not the
	// current password it asked to replace.
	ErrCurrentPasswordInvalid = errors.New("current password is not correct")
	// ErrUnauthenticated means there is no live admin session behind the cookie.
	ErrUnauthenticated = errors.New("no valid admin session")
	// ErrRateLimited means one of the two login budgets is exhausted.
	ErrRateLimited = errors.New("admin login rate limited")
)

// timingEqualiser is a bcrypt hash of a value nobody knows, compared against when
// the email is unknown so an unknown address costs the same wall time as a wrong
// password. Without it the response body is uniform but the clock is not.
//
//nolint:gosec // a deliberate non-credential used only for constant-work comparison
const timingEqualiser = "$2a$12$zIz7LKJC5RtCYAo1hNbpP.gCq2iyh8.zfUvhmlhzsScrjFx7qUBQS"

// AuthSettings are the sign-in knobs, kept local so this domain does not depend on
// the deployment configuration struct.
type AuthSettings struct {
	// SessionTTL is an absolute lifetime: a session is never slid forward, so a
	// laptop left behind closes itself (ADR 0024 §2).
	SessionTTL time.Duration
	// IPWindow/IPLimit bound attempts from one address; EmailWindow/EmailLimit
	// bound attempts against one address. Two keys, because either alone is
	// trivially evaded (ADR 0024 §4).
	IPWindow    time.Duration
	IPLimit     int
	EmailWindow time.Duration
	EmailLimit  int
}

// Issued is what a successful login hands the transport: the raw token for the
// cookie and the body the panel parses. The token is deliberately not part of the
// response DTO, so no serialization path can leak it (ADR 0024 §2).
type Issued struct {
	SessionToken string
	Response     *dto.SessionResponse
}

// Auth is the panel's sign-in surface. It is a second interface beside Service
// because the migration command builds the admin domain without Redis, and a
// bootstrap must not need the login limiter to exist.
type Auth interface {
	// Login verifies a password and opens a session. Unknown email, wrong password
	// and inactive admin all answer ErrCredentialsInvalid with the same body.
	Login(ctx context.Context, request *dto.LoginRequest, clientIP string) (*Issued, error)
	// Logout revokes the session behind a token. It is idempotent.
	Logout(ctx context.Context, sessionToken string) error
	// Session resolves a cookie value into the identity behind it.
	Session(ctx context.Context, sessionToken string) (*dto.SessionResponse, error)
	// ChangeOwnPassword replaces the signed-in admin's password after re-checking
	// the current one.
	ChangeOwnPassword(ctx context.Context, actor audit.Actor, request *dto.ChangePasswordRequest) error
}

type authService struct {
	repo     repository.Repository
	settings AuthSettings
	limiter  ratelimit.Limiter
	clock    clock.Clock
	logger   *zap.Logger
}

// NewAuth builds the sign-in service; Fx injects every port.
func NewAuth(
	repo repository.Repository,
	settings AuthSettings,
	limiter ratelimit.Limiter,
	clock clock.Clock,
	logger *zap.Logger,
) Auth {
	return &authService{repo: repo, settings: settings, limiter: limiter, clock: clock, logger: logger}
}

// Login checks the two budgets, then the credential, then opens a session. The
// budgets are checked first so a guess flood costs no bcrypt work, and both are
// checked before the lookup so neither dimension can be skipped.
func (s *authService) Login(
	ctx context.Context,
	request *dto.LoginRequest,
	clientIP string,
) (*Issued, error) {
	if err := s.allow(ctx, "ip", clientIP, s.settings.IPLimit, s.settings.IPWindow); err != nil {
		return nil, err
	}
	emailKey := strings.ToLower(strings.TrimSpace(request.Email))
	if err := s.allow(ctx, "email", emailKey, s.settings.EmailLimit, s.settings.EmailWindow); err != nil {
		return nil, err
	}

	admin, err := s.authenticate(ctx, request)
	if err != nil {
		return nil, err
	}

	raw, hash, err := securetoken.New()
	if err != nil {
		s.logger.Error("mint admin session token", zap.Error(err))
		return nil, fmt.Errorf("mint admin session token: %w", err)
	}

	expiresAt := s.clock.Now().Add(s.settings.SessionTTL)
	session := &entity.Session{TokenHash: hash, AdminID: admin.ID, ExpiresAt: expiresAt}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("create admin session: %w", err)
	}

	s.logger.Info("admin signed in",
		zap.Uint("admin_id", admin.ID),
		zap.String("email", admin.Email),
		zap.String("role", string(admin.Role)),
	)
	return &Issued{SessionToken: raw, Response: sessionResponse(*admin, expiresAt)}, nil
}

// authenticate resolves the credential or answers the one uniform sentinel. Every
// failure path pays for one bcrypt comparison, so the answer does not leak through
// timing either (ADR 0024 §4).
func (s *authService) authenticate(ctx context.Context, request *dto.LoginRequest) (*entity.Admin, error) {
	email, err := canonicalEmail(request.Email)
	if err != nil {
		compareTimingEqualiser(request.Password)
		return nil, ErrCredentialsInvalid
	}

	admin, err := s.repo.AdminByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if admin == nil || !admin.IsActive {
		compareTimingEqualiser(request.Password)
		return nil, ErrCredentialsInvalid
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(request.Password)) != nil {
		return nil, ErrCredentialsInvalid
	}
	return admin, nil
}

// allow asks the limiter one question per dimension. A limiter error is returned,
// never swallowed: treating an unreachable Redis as "allowed" would remove exactly
// the bound that exists to stop guessing (ADR 0024 §4).
func (s *authService) allow(ctx context.Context, dimension, key string, limit int, window time.Duration) error {
	allowed, err := s.limiter.Allow(ctx, "admin:login:"+dimension+":"+key, limit, window)
	if err != nil {
		return fmt.Errorf("admin login rate limit: %w", err)
	}
	if !allowed {
		return ErrRateLimited
	}
	return nil
}

// Logout revokes the session. The cookie is cleared by the transport too, but the
// row is what makes the old value useless (ADR 0024 §5).
func (s *authService) Logout(ctx context.Context, sessionToken string) error {
	if strings.TrimSpace(sessionToken) == "" {
		return nil
	}
	if err := s.repo.RevokeSession(ctx, securetoken.Hash(sessionToken), s.clock.Now()); err != nil {
		return fmt.Errorf("revoke admin session: %w", err)
	}
	return nil
}

// Session resolves a cookie value into the identity the guard stores. An expired,
// revoked or inactive-admin session is ErrUnauthenticated, the same answer as a
// missing cookie.
func (s *authService) Session(ctx context.Context, sessionToken string) (*dto.SessionResponse, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return nil, ErrUnauthenticated
	}

	live, err := s.repo.Session(ctx, securetoken.Hash(sessionToken), s.clock.Now())
	if err != nil {
		return nil, err
	}
	if live == nil {
		return nil, ErrUnauthenticated
	}
	return sessionResponse(live.Admin, live.Session.ExpiresAt), nil
}

// ChangeOwnPassword re-checks the current password before rewriting the hash. The
// session is not revoked: the operator asked for this change from this browser, and
// the other sessions of the same admin are their own (ADR 0024 §5 is about reset
// by an owner, not about self-service).
func (s *authService) ChangeOwnPassword(
	ctx context.Context,
	actor audit.Actor,
	request *dto.ChangePasswordRequest,
) error {
	if err := requireActor(actor); err != nil {
		return err
	}

	admin, err := requireAdmin(ctx, s.repo, actor.ID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(request.CurrentPassword)) != nil {
		return ErrCurrentPasswordInvalid
	}
	if err := checkPassword(admin.Email, request.NewPassword); err != nil {
		return fmt.Errorf("%w: %v", ErrPasswordWeak, err)
	}

	hash, err := hashPassword(request.NewPassword)
	if err != nil {
		s.logger.Error("hash admin password", zap.Error(err))
		return err
	}
	if err := s.repo.ChangePassword(ctx, admin.ID, hash); err != nil {
		if errors.Is(err, repository.ErrAdminNotFound) {
			// The session was live a moment ago, so a missing row means the account
			// is gone: answer it like any other dead session.
			return ErrUnauthenticated
		}
		return err
	}

	s.logger.Info("admin changed own password", zap.Uint("admin_id", admin.ID))
	return nil
}

// sessionResponse maps a row onto the shape both login and the session read answer
// with, so the panel parses one contract (ADR 0024 §3).
func sessionResponse(admin entity.Admin, expiresAt time.Time) *dto.SessionResponse {
	return &dto.SessionResponse{
		AdminID:   admin.ID,
		Email:     admin.Email,
		Role:      string(admin.Role),
		ExpiresAt: expiresAt,
	}
}

// compareTimingEqualiser spends one bcrypt comparison on a value nobody knows, so
// the unknown-email path is as expensive as the wrong-password path.
func compareTimingEqualiser(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(timingEqualiser), []byte(password))
}
