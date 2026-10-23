package service_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/service"
	notificationdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	packagesdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	packageservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/securetoken"
)

// --- fakes -----------------------------------------------------------------

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeLimiter struct{ blocked []string }

func (l *fakeLimiter) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, error) {
	for _, prefix := range l.blocked {
		if strings.HasPrefix(key, prefix) {
			return false, nil
		}
	}
	return true, nil
}

type fakeOutbox struct{ messages []notificationdto.Message }

func (o *fakeOutbox) Enqueue(_ context.Context, message notificationdto.Message) error {
	o.messages = append(o.messages, message)
	return nil
}

type fakeCatalog struct {
	packages []packagesdto.PackageResponse
	typed    []packagesentity.Package
}

func (c *fakeCatalog) ListPackages(context.Context) ([]packagesdto.PackageResponse, error) {
	return c.packages, nil
}

func (c *fakeCatalog) GetPackage(_ context.Context, id uint) (*packagesentity.Package, error) {
	for _, pkg := range c.typed {
		if pkg.ID == id {
			return &pkg, nil
		}
	}
	return nil, packageservice.ErrPackageNotFound
}

func (c *fakeCatalog) Seed(context.Context) (int64, error) { return 0, nil }

type fakeRepository struct {
	links    []entity.MagicLinkToken
	sessions []entity.Session
}

func (r *fakeRepository) CreateMagicLink(_ context.Context, link repository.NewMagicLink) error {
	r.links = append(r.links, entity.MagicLinkToken{
		TokenHash:   link.TokenHash,
		PhoneNumber: link.PhoneNumber,
		Purpose:     link.Purpose,
		PackageID:   link.PackageID,
		ReturnURL:   link.ReturnURL,
		ExpiresAt:   link.ExpiresAt,
	})
	return nil
}

func (r *fakeRepository) ConsumeMagicLink(
	_ context.Context,
	hash string,
	now time.Time,
) (*entity.MagicLinkToken, error) {
	for i := range r.links {
		link := &r.links[i]
		if link.TokenHash != hash || link.UsedAt != nil || !link.ExpiresAt.After(now) {
			continue
		}
		used := now
		link.UsedAt = &used
		clone := *link
		return &clone, nil
	}
	return nil, nil
}

func (r *fakeRepository) CreateSession(_ context.Context, session *entity.Session) error {
	r.sessions = append(r.sessions, *session)
	return nil
}

func (r *fakeRepository) Session(_ context.Context, hash string, now time.Time) (*entity.Session, error) {
	for i := range r.sessions {
		session := &r.sessions[i]
		if session.TokenHash == hash && session.RevokedAt == nil && session.ExpiresAt.After(now) {
			clone := *session
			return &clone, nil
		}
	}
	return nil, nil
}

func (r *fakeRepository) RevokeSession(_ context.Context, hash string, now time.Time) error {
	for i := range r.sessions {
		if r.sessions[i].TokenHash == hash && r.sessions[i].RevokedAt == nil {
			revoked := now
			r.sessions[i].RevokedAt = &revoked
		}
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

type fixture struct {
	service service.Service
	repo    *fakeRepository
	outbox  *fakeOutbox
	clock   *fakeClock
	limiter *fakeLimiter
	catalog *fakeCatalog
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	f := &fixture{
		repo:    &fakeRepository{},
		outbox:  &fakeOutbox{},
		clock:   &fakeClock{now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)},
		limiter: &fakeLimiter{},
		catalog: &fakeCatalog{packages: []packagesdto.PackageResponse{{ID: 7, Name: "Paket Harian"}}},
	}

	f.service = service.NewService(
		f.repo,
		f.catalog,
		f.outbox,
		f.limiter,
		f.clock,
		service.Settings{
			MagicLinkTTL:       15 * time.Minute,
			SessionTTL:         30 * 24 * time.Hour,
			AppBaseURL:         "https://app.example",
			MagicLinkPath:      "/masuk/tautan",
			LoginPageAllowlist: []string{"http://192.168.88.1/login"},
			PhoneWindow:        5 * time.Minute,
			PhoneLimit:         1,
			IPWindow:           5 * time.Minute,
			IPLimit:            10,
		},
		zap.NewNop(),
	)
	return f
}

// tokenFrom pulls the token out of the Magic Link SMS the fake outbox recorded.
func (f *fixture) tokenFrom(t *testing.T, index int) string {
	t.Helper()

	if len(f.outbox.messages) <= index {
		t.Fatalf("expected at least %d queued SMS, got %d", index+1, len(f.outbox.messages))
	}

	text := f.outbox.messages[index].Text
	start := strings.Index(text, "https://app.example/")
	if start == -1 {
		t.Fatalf("SMS %q does not contain a Magic Link", text)
	}
	end := strings.Index(text[start:], " ")
	if end == -1 {
		t.Fatalf("SMS %q has an unterminated Magic Link", text)
	}

	link, err := url.Parse(text[start : start+end])
	if err != nil {
		t.Fatalf("parse Magic Link: %v", err)
	}
	return link.Query().Get("token")
}

// --- tests -----------------------------------------------------------------

func TestRequestMagicLinkQueuesOneSmsToTheCanonicalNumber(t *testing.T) {
	f := newFixture(t)

	if err := f.service.RequestMagicLink(context.Background(),
		&dto.RequestMagicLinkRequest{PhoneNumber: "0812-3456-7890"}, "203.0.113.9"); err != nil {
		t.Fatalf("request magic link: %v", err)
	}

	if len(f.outbox.messages) != 1 {
		t.Fatalf("queued %d SMS, want 1", len(f.outbox.messages))
	}
	if got := f.outbox.messages[0].PhoneNumber; got != "6281234567890" {
		t.Fatalf("SMS went to %q, want the canonical Phone Number", got)
	}

	token := f.tokenFrom(t, 0)
	if token == "" {
		t.Fatal("the SMS carries no token")
	}

	link := f.repo.links[0]
	if link.TokenHash != securetoken.Hash(token) {
		t.Fatal("the stored hash is not the hash of the token in the SMS")
	}
	if link.TokenHash == token {
		t.Fatal("the raw token was stored")
	}
	if link.Purpose != entity.PurposeAccount {
		t.Fatalf("purpose = %q, want account", link.Purpose)
	}
	if got := link.ExpiresAt.Sub(f.clock.now); got != 15*time.Minute {
		t.Fatalf("link lifetime = %v, want 15m", got)
	}
}

func TestRequestMagicLinkForACheckoutKeepsThePackage(t *testing.T) {
	f := newFixture(t)
	packageID := uint(7)

	if err := f.service.RequestMagicLink(context.Background(),
		&dto.RequestMagicLinkRequest{PhoneNumber: "081234567890", PackageID: &packageID},
		"203.0.113.9"); err != nil {
		t.Fatalf("request magic link: %v", err)
	}

	if f.repo.links[0].Purpose != entity.PurposeCheckout {
		t.Fatalf("purpose = %q, want checkout", f.repo.links[0].Purpose)
	}
	if f.repo.links[0].PackageID == nil || *f.repo.links[0].PackageID != packageID {
		t.Fatal("the Package was not stored with the link")
	}
}

func TestRequestMagicLinkRejectsAnUnknownPackage(t *testing.T) {
	f := newFixture(t)
	unknown := uint(99)

	err := f.service.RequestMagicLink(context.Background(),
		&dto.RequestMagicLinkRequest{PhoneNumber: "081234567890", PackageID: &unknown},
		"203.0.113.9")
	if !errors.Is(err, service.ErrPackageUnknown) {
		t.Fatalf("error = %v, want ErrPackageUnknown", err)
	}
	if len(f.outbox.messages) != 0 {
		t.Fatal("no SMS may be sent for a rejected request")
	}
}

func TestRequestMagicLinkRejectsAnInvalidPhoneNumber(t *testing.T) {
	f := newFixture(t)

	err := f.service.RequestMagicLink(context.Background(),
		&dto.RequestMagicLinkRequest{PhoneNumber: "12345"}, "203.0.113.9")
	if !errors.Is(err, phone.ErrInvalid) {
		t.Fatalf("error = %v, want phone.ErrInvalid", err)
	}
}

func TestRequestMagicLinkSuppressesTheSmsButKeepsTheAnswerForAPhoneOverItsLimit(t *testing.T) {
	f := newFixture(t)
	f.limiter.blocked = []string{"auth:magic:phone:"}

	err := f.service.RequestMagicLink(context.Background(),
		&dto.RequestMagicLinkRequest{PhoneNumber: "081234567890"}, "203.0.113.9")
	if err != nil {
		t.Fatalf("a Phone over its limit must not be distinguishable, got %v", err)
	}
	if len(f.outbox.messages) != 0 {
		t.Fatalf("queued %d SMS, want 0", len(f.outbox.messages))
	}
	if len(f.repo.links) != 0 {
		t.Fatal("no token may be stored when no SMS is sent")
	}
}

func TestRequestMagicLinkReportsAnIpLimit(t *testing.T) {
	f := newFixture(t)
	f.limiter.blocked = []string{"auth:magic:ip:"}

	err := f.service.RequestMagicLink(context.Background(),
		&dto.RequestMagicLinkRequest{PhoneNumber: "081234567890"}, "203.0.113.9")
	if !errors.Is(err, service.ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
}

func TestRequestMagicLinkDropsAnUnlistedReturnTarget(t *testing.T) {
	f := newFixture(t)

	if err := f.service.RequestMagicLink(context.Background(), &dto.RequestMagicLinkRequest{
		PhoneNumber: "081234567890",
		ReturnURL:   "https://evil.example/steal?token=1",
	}, "203.0.113.9"); err != nil {
		t.Fatalf("request magic link: %v", err)
	}
	if f.repo.links[0].ReturnURL != "" {
		t.Fatalf("return target %q survived; it must be dropped", f.repo.links[0].ReturnURL)
	}
}

func TestRequestMagicLinkKeepsAListedReturnTarget(t *testing.T) {
	f := newFixture(t)
	const allowed = "http://192.168.88.1/login?mac=aa-bb"

	if err := f.service.RequestMagicLink(context.Background(), &dto.RequestMagicLinkRequest{
		PhoneNumber: "081234567890",
		ReturnURL:   allowed,
	}, "203.0.113.9"); err != nil {
		t.Fatalf("request magic link: %v", err)
	}
	if f.repo.links[0].ReturnURL != allowed {
		t.Fatalf("return target = %q, want %q", f.repo.links[0].ReturnURL, allowed)
	}
}

func TestExchangeMagicLinkOpensOneSessionAndBurnsTheToken(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.service.RequestMagicLink(ctx,
		&dto.RequestMagicLinkRequest{PhoneNumber: "081234567890"}, "203.0.113.9"); err != nil {
		t.Fatalf("request magic link: %v", err)
	}
	token := f.tokenFrom(t, 0)

	session, err := f.service.ExchangeMagicLink(ctx, token)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if session.PhoneNumber != "6281234567890" {
		t.Fatalf("session Phone Number = %q", session.PhoneNumber)
	}
	if session.SessionToken == "" {
		t.Fatal("exchange returned no session token")
	}
	if session.SessionToken == token {
		t.Fatal("the session token must not be the Magic Link token")
	}
	if len(f.repo.sessions) != 1 {
		t.Fatalf("stored %d sessions, want 1", len(f.repo.sessions))
	}
	if f.repo.sessions[0].TokenHash != securetoken.Hash(session.SessionToken) {
		t.Fatal("the raw session token was stored")
	}

	if _, err := f.service.ExchangeMagicLink(ctx, token); !errors.Is(err, service.ErrLinkInvalid) {
		t.Fatalf("second exchange error = %v, want ErrLinkInvalid", err)
	}
}

func TestExchangeMagicLinkRejectsAnExpiredToken(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.service.RequestMagicLink(ctx,
		&dto.RequestMagicLinkRequest{PhoneNumber: "081234567890"}, "203.0.113.9"); err != nil {
		t.Fatalf("request magic link: %v", err)
	}
	token := f.tokenFrom(t, 0)

	f.clock.now = f.clock.now.Add(16 * time.Minute)

	if _, err := f.service.ExchangeMagicLink(ctx, token); !errors.Is(err, service.ErrLinkInvalid) {
		t.Fatalf("error = %v, want ErrLinkInvalid", err)
	}
	if len(f.repo.sessions) != 0 {
		t.Fatal("an expired link must not open a session")
	}
}

func TestAuthenticateAndSignOut(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.service.RequestMagicLink(ctx,
		&dto.RequestMagicLinkRequest{PhoneNumber: "081234567890"}, "203.0.113.9"); err != nil {
		t.Fatalf("request magic link: %v", err)
	}
	session, err := f.service.ExchangeMagicLink(ctx, f.tokenFrom(t, 0))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	identity, err := f.service.Authenticate(ctx, session.SessionToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if identity.PhoneNumber != "6281234567890" {
		t.Fatalf("identity = %q", identity.PhoneNumber)
	}

	if err := f.service.SignOut(ctx, session.SessionToken); err != nil {
		t.Fatalf("sign out: %v", err)
	}
	if _, err := f.service.Authenticate(ctx, session.SessionToken); !errors.Is(err, service.ErrUnauthenticated) {
		t.Fatalf("after sign-out error = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateRejectsAnExpiredOrUnknownSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.service.Authenticate(ctx, "not-a-session"); !errors.Is(err, service.ErrUnauthenticated) {
		t.Fatalf("unknown session error = %v, want ErrUnauthenticated", err)
	}
	if _, err := f.service.Authenticate(ctx, ""); !errors.Is(err, service.ErrUnauthenticated) {
		t.Fatalf("empty session error = %v, want ErrUnauthenticated", err)
	}
}
