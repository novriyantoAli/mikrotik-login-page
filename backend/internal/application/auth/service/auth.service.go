// Package service owns the passwordless entry rules: a Magic Link is requested
// with a Phone Number, redeemed exactly once, and turned into an opaque session
// (ADR 0011). Nothing in this package knows about cookies or HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/repository"
	notificationdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	packagesservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/ratelimit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/securetoken"
)

// Errors the handler maps onto HTTP statuses. They are sentinels, not strings,
// so a transport never has to match on message text.
var (
	// ErrLinkInvalid covers unknown, already used and expired links alike: one
	// message, no information about which (ADR 0011).
	ErrLinkInvalid = errors.New("magic link is not valid")
	// ErrUnauthenticated means there is no live session.
	ErrUnauthenticated = errors.New("no valid session")
	// ErrRateLimited means the client IP is over its request budget.
	ErrRateLimited = errors.New("rate limited")
	// ErrPackageUnknown means the requested Package is not in the catalog; it is
	// a bad request, not a server failure.
	ErrPackageUnknown = errors.New("package not found")
)

// Settings are the auth knobs the service needs, kept local so the domain does
// not depend on the deployment configuration struct.
type Settings struct {
	MagicLinkTTL       time.Duration
	SessionTTL         time.Duration
	AppBaseURL         string
	MagicLinkPath      string
	LoginPageAllowlist []string
	PhoneWindow        time.Duration
	PhoneLimit         int
	IPWindow           time.Duration
	IPLimit            int
}

// Service is the auth domain's public surface. Other domains authorize with
// Authenticate, never by reading the sessions table.
type Service interface {
	// RequestMagicLink issues a link and queues the SMS. It reports nil for a
	// Phone Number that is over its own budget, because the answer must be
	// identical for every caller (PRD point 10).
	RequestMagicLink(ctx context.Context, request *dto.RequestMagicLinkRequest, clientIP string) error
	// ExchangeMagicLink redeems a token and returns the session to set as a
	// cookie, plus the intent stored with the link.
	ExchangeMagicLink(ctx context.Context, token string) (*dto.ExchangeMagicLinkResponse, error)
	// Authenticate resolves a session cookie value into the Phone Number it
	// belongs to.
	Authenticate(ctx context.Context, sessionToken string) (*dto.SessionResponse, error)
	// SignOut revokes the session behind a cookie value. It is idempotent.
	SignOut(ctx context.Context, sessionToken string) error
}

type service struct {
	repo     repository.Repository
	catalog  packagesservice.Service
	outbox   notificationdto.Outbox
	limiter  ratelimit.Limiter
	clock    clock.Clock
	settings Settings
	logger   *zap.Logger
}

// NewService builds the auth service; Fx injects every port.
func NewService(
	repo repository.Repository,
	catalog packagesservice.Service,
	outbox notificationdto.Outbox,
	limiter ratelimit.Limiter,
	clock clock.Clock,
	settings Settings,
	logger *zap.Logger,
) Service {
	return &service{
		repo:     repo,
		catalog:  catalog,
		outbox:   outbox,
		limiter:  limiter,
		clock:    clock,
		settings: settings,
		logger:   logger,
	}
}

func (s *service) RequestMagicLink(
	ctx context.Context,
	request *dto.RequestMagicLinkRequest,
	clientIP string,
) error {
	normalized, err := phone.Normalize(request.PhoneNumber)
	if err != nil {
		// The handler maps phone.ErrInvalid onto a 400.
		return fmt.Errorf("normalize Phone Number: %w", err)
	}

	purpose := entity.PurposeAccount
	var packageID *uint
	if request.PackageID != nil {
		if err := s.requirePackage(ctx, *request.PackageID); err != nil {
			return err
		}
		purpose = entity.PurposeCheckout
		packageID = request.PackageID
	}

	// The IP budget is answered with a stable error; the per-Phone budget is not,
	// because telling a caller "this number asked too often" would confirm the
	// number exists (PRD point 10/65).
	ipAllowed, err := s.limiter.Allow(ctx,
		"auth:magic:ip:"+clientIP, s.settings.IPLimit, s.settings.IPWindow)
	if err != nil {
		return fmt.Errorf("rate limit magic link by ip: %w", err)
	}
	if !ipAllowed {
		return ErrRateLimited
	}

	phoneAllowed, err := s.limiter.Allow(ctx,
		"auth:magic:phone:"+securetoken.Hash(normalized), s.settings.PhoneLimit, s.settings.PhoneWindow)
	if err != nil {
		return fmt.Errorf("rate limit magic link by phone: %w", err)
	}
	if !phoneAllowed {
		// One SMS per window: the answer stays the same, only the SMS is skipped.
		s.logger.Info("magic link request suppressed by the per-Phone limit",
			zap.String("phone_number", phone.Mask(normalized)),
		)
		return nil
	}

	rawToken, tokenHash, err := securetoken.New()
	if err != nil {
		return fmt.Errorf("generate magic link token: %w", err)
	}
	if err := s.repo.CreateMagicLink(ctx, repository.NewMagicLink{
		TokenHash:   tokenHash,
		PhoneNumber: normalized,
		Purpose:     purpose,
		PackageID:   packageID,
		ReturnURL:   s.allowedReturn(request.ReturnURL),
		ExpiresAt:   s.clock.Now().Add(s.settings.MagicLinkTTL),
	}); err != nil {
		return fmt.Errorf("store magic link: %w", err)
	}

	link := s.magicLinkURL(rawToken)
	if err := s.outbox.Enqueue(ctx, notificationdto.Message{
		PhoneNumber: normalized,
		Text:        magicLinkMessage(link, s.settings.MagicLinkTTL),
	}); err != nil {
		// The token row exists but no SMS will arrive. Remove the ambiguity: the
		// caller answers the same success, and the buyer simply asks again.
		return fmt.Errorf("queue magic link sms: %w", err)
	}

	s.logger.Info("magic link issued",
		zap.String("phone_number", phone.Mask(normalized)),
		zap.String("purpose", string(purpose)),
	)
	return nil
}

func (s *service) ExchangeMagicLink(ctx context.Context, token string) (*dto.ExchangeMagicLinkResponse, error) {
	now := s.clock.Now()

	consumed, err := s.repo.ConsumeMagicLink(ctx, securetoken.Hash(token), now)
	if err != nil {
		return nil, fmt.Errorf("consume magic link: %w", err)
	}
	if consumed == nil {
		return nil, ErrLinkInvalid
	}

	rawSession, sessionHash, err := securetoken.New()
	if err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}

	expiresAt := now.Add(s.settings.SessionTTL)
	if err := s.repo.CreateSession(ctx, &entity.Session{
		TokenHash:   sessionHash,
		PhoneNumber: consumed.PhoneNumber,
		ExpiresAt:   expiresAt,
	}); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	s.logger.Info("session opened",
		zap.String("phone_number", phone.Mask(consumed.PhoneNumber)),
		zap.String("purpose", string(consumed.Purpose)),
	)

	return &dto.ExchangeMagicLinkResponse{
		// The raw session token is returned only so the handler can put it in the
		// cookie; it is never stored or logged anywhere else.
		SessionToken: rawSession,
		SessionResponse: dto.SessionResponse{
			PhoneNumber: consumed.PhoneNumber,
			ExpiresAt:   expiresAt.UTC().Format(time.RFC3339),
		},
		Purpose:   string(consumed.Purpose),
		PackageID: consumed.PackageID,
		ReturnURL: consumed.ReturnURL,
	}, nil
}

func (s *service) Authenticate(ctx context.Context, sessionToken string) (*dto.SessionResponse, error) {
	if sessionToken == "" {
		return nil, ErrUnauthenticated
	}

	session, err := s.repo.Session(ctx, securetoken.Hash(sessionToken), s.clock.Now())
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}
	if session == nil {
		return nil, ErrUnauthenticated
	}

	return &dto.SessionResponse{
		PhoneNumber: session.PhoneNumber,
		ExpiresAt:   session.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *service) SignOut(ctx context.Context, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}

	if err := s.repo.RevokeSession(ctx, securetoken.Hash(sessionToken), s.clock.Now()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// requirePackage rejects a checkout for a Package the catalog does not sell, so
// a Magic Link can never carry an unknown intent.
func (s *service) requirePackage(ctx context.Context, packageID uint) error {
	packages, err := s.catalog.ListPackages(ctx)
	if err != nil {
		return fmt.Errorf("read package catalog: %w", err)
	}
	for _, pkg := range packages {
		if pkg.ID == packageID {
			return nil
		}
	}
	return ErrPackageUnknown
}

// allowedReturn keeps a return target only when it matches the deployment's
// Login Page allowlist. The check happens when the link is issued, so redeeming
// never trusts a value from the URL (ADR 0011).
func (s *service) allowedReturn(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}

	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	for _, allowed := range s.settings.LoginPageAllowlist {
		target, err := url.Parse(strings.TrimSpace(allowed))
		if err != nil || target.Scheme == "" || target.Host == "" {
			continue
		}
		if !strings.EqualFold(parsed.Scheme, target.Scheme) || !strings.EqualFold(parsed.Host, target.Host) {
			continue
		}
		if !strings.HasPrefix(parsed.Path, target.Path) {
			continue
		}

		parsed.Fragment = ""
		return parsed.String()
	}

	s.logger.Warn("return target ignored: not in the Login Page allowlist",
		zap.String("return_host", parsed.Host),
	)
	return ""
}

func (s *service) magicLinkURL(token string) string {
	base := strings.TrimSuffix(s.settings.AppBaseURL, "/")
	path := s.settings.MagicLinkPath
	if path == "" {
		path = "/masuk/tautan"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path + "?token=" + url.QueryEscape(token)
}

func magicLinkMessage(link string, ttl time.Duration) string {
	return fmt.Sprintf(
		"Masuk ke Aplikasi Pembelian: %s Berlaku %s. Abaikan pesan ini bila Anda tidak memintanya.",
		link,
		formatDuration(ttl),
	)
}

// formatDuration renders a lifetime the way the SMS reads it: whole minutes.
func formatDuration(duration time.Duration) string {
	if minutes := int(duration.Minutes()); minutes > 0 {
		return fmt.Sprintf("%d menit", minutes)
	}
	return "beberapa saat"
}
