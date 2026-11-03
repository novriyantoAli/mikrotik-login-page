package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/securetoken"
)

// panelPassword is the fixture credential every sign-in test hashes to cost 12.
const panelPassword = "kata-sandi-yang-cukup-panjang"

// fakeLimiter answers per key and records the keys it was asked about, so a test
// can prove both dimensions were checked rather than one standing in for both.
type fakeLimiter struct {
	deny map[string]bool
	err  error
	keys []string
}

func (l *fakeLimiter) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, error) {
	l.keys = append(l.keys, key)
	if l.err != nil {
		return false, l.err
	}
	return !l.deny[key], nil
}

// activeAdmin seeds one bcrypt-hashed admin, the same shape `migrate` writes.
func activeAdmin(t *testing.T, password string, role entity.Role) entity.Admin {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), service.PasswordCost)
	if err != nil {
		t.Fatalf("hash fixture password: %v", err)
	}
	return entity.Admin{
		ID:           7,
		Email:        "owner@example.test",
		PasswordHash: string(hash),
		Role:         role,
		IsActive:     true,
	}
}

func newAuth(repo *fakeRepository, limiter *fakeLimiter, clock fixedClock) service.Auth {
	return service.NewAuth(repo, service.AuthSettings{
		SessionTTL:  12 * time.Hour,
		IPWindow:    5 * time.Minute,
		IPLimit:     10,
		EmailWindow: 5 * time.Minute,
		EmailLimit:  5,
	}, limiter, clock, zap.NewNop())
}

// TestLoginOpensASessionForTheRightCredential is ADR 0024 §2: the raw token goes
// to the cookie and only its hash is stored, with an absolute expiry the clock
// decides.
func TestLoginOpensASessionForTheRightCredential(t *testing.T) {
	repo := &fakeRepository{storedAdmins: []entity.Admin{activeAdmin(t, panelPassword, entity.RoleOwner)}}
	limiter := &fakeLimiter{deny: map[string]bool{}}
	issued, err := newAuth(repo, limiter, testClock).Login(context.Background(), &dto.LoginRequest{
		Email:    "  Owner@Example.TEST ",
		Password: panelPassword,
	}, "203.0.113.5")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if issued.SessionToken == "" {
		t.Fatal("login returned no session token")
	}
	if len(repo.sessions) != 1 {
		t.Fatalf("stored sessions = %d, want 1", len(repo.sessions))
	}
	stored := repo.sessions[0]
	if stored.TokenHash != securetoken.Hash(issued.SessionToken) {
		t.Fatal("the stored hash is not the hash of the issued token")
	}
	if stored.TokenHash == issued.SessionToken {
		t.Fatal("the raw token was stored; only its hash may be")
	}
	wantExpiry := testClock.Now().Add(12 * time.Hour)
	if !stored.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("expires_at = %s, want the absolute TTL %s", stored.ExpiresAt, wantExpiry)
	}
	if issued.Response.Email != "owner@example.test" || issued.Response.Role != string(entity.RoleOwner) {
		t.Fatalf("response = %+v, want the canonical owner", issued.Response)
	}
	if !issued.Response.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("response expires_at = %s, want %s", issued.Response.ExpiresAt, wantExpiry)
	}
}

// TestLoginAnswersEveryCredentialFailureIdentically is ADR 0024 §4: unknown email,
// wrong password and a deactivated admin are one value, so no caller can enumerate
// admins.
func TestLoginAnswersEveryCredentialFailureIdentically(t *testing.T) {
	inactive := activeAdmin(t, panelPassword, entity.RoleOwner)
	inactive.IsActive = false

	cases := map[string]struct {
		repo     *fakeRepository
		password string
		email    string
	}{
		"unknown email": {
			repo:     &fakeRepository{storedAdmins: []entity.Admin{activeAdmin(t, panelPassword, entity.RoleOwner)}},
			email:    "nobody@example.test",
			password: panelPassword,
		},
		"wrong password": {
			repo:     &fakeRepository{storedAdmins: []entity.Admin{activeAdmin(t, panelPassword, entity.RoleOwner)}},
			email:    "owner@example.test",
			password: "kata-sandi-yang-salah-sekali",
		},
		"deactivated admin": {
			repo:     &fakeRepository{storedAdmins: []entity.Admin{inactive}},
			email:    "owner@example.test",
			password: panelPassword,
		},
		"unparseable email": {
			repo:     &fakeRepository{},
			email:    "not-an-email",
			password: panelPassword,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			limiter := &fakeLimiter{deny: map[string]bool{}}
			_, err := newAuth(testCase.repo, limiter, testClock).Login(context.Background(), &dto.LoginRequest{
				Email:    testCase.email,
				Password: testCase.password,
			}, "203.0.113.6")

			if !errors.Is(err, service.ErrCredentialsInvalid) {
				t.Fatalf("error = %v, want ErrCredentialsInvalid", err)
			}
			if len(testCase.repo.sessions) != 0 {
				t.Fatal("a refused login opened a session")
			}
		})
	}
}

// TestLoginChecksBothRateLimitDimensions: an IP budget and an email budget, both
// asked before the credential is, so neither can be skipped.
func TestLoginChecksBothRateLimitDimensions(t *testing.T) {
	repo := &fakeRepository{storedAdmins: []entity.Admin{activeAdmin(t, panelPassword, entity.RoleOwner)}}

	ipDenied := &fakeLimiter{deny: map[string]bool{"admin:login:ip:203.0.113.7": true}}
	if _, err := newAuth(repo, ipDenied, testClock).Login(context.Background(), &dto.LoginRequest{
		Email: "owner@example.test", Password: panelPassword,
	}, "203.0.113.7"); !errors.Is(err, service.ErrRateLimited) {
		t.Fatalf("IP-limited login error = %v, want ErrRateLimited", err)
	}
	if len(ipDenied.keys) != 1 || ipDenied.keys[0] != "admin:login:ip:203.0.113.7" {
		t.Fatalf("keys asked = %v, want the IP key only", ipDenied.keys)
	}

	emailDenied := &fakeLimiter{deny: map[string]bool{"admin:login:email:owner@example.test": true}}
	if _, err := newAuth(repo, emailDenied, testClock).Login(context.Background(), &dto.LoginRequest{
		Email: "Owner@Example.test", Password: panelPassword,
	}, "203.0.113.8"); !errors.Is(err, service.ErrRateLimited) {
		t.Fatalf("email-limited login error = %v, want ErrRateLimited", err)
	}
	if len(emailDenied.keys) != 2 {
		t.Fatalf("keys asked = %v, want the IP key and then the email key", emailDenied.keys)
	}
}

// TestLoginDoesNotSwallowALimiterFailure: an unreachable Redis must not read as
// "allowed", because that removes the bound the limiter exists for.
func TestLoginDoesNotSwallowALimiterFailure(t *testing.T) {
	repo := &fakeRepository{storedAdmins: []entity.Admin{activeAdmin(t, panelPassword, entity.RoleOwner)}}
	limiter := &fakeLimiter{err: errors.New("redis is down")}

	_, err := newAuth(repo, limiter, testClock).Login(context.Background(), &dto.LoginRequest{
		Email: "owner@example.test", Password: panelPassword,
	}, "203.0.113.9")
	if err == nil || errors.Is(err, service.ErrRateLimited) || errors.Is(err, service.ErrCredentialsInvalid) {
		t.Fatalf("error = %v, want the limiter failure itself", err)
	}
	if len(repo.sessions) != 0 {
		t.Fatal("a login with a broken limiter opened a session")
	}
}

// TestSessionStopsMatchingWhenExpiredOrRevoked is the TTL acceptance criterion:
// the absolute expiry is the clock's answer, and logout revokes immediately.
func TestSessionStopsMatchingWhenExpiredOrRevoked(t *testing.T) {
	repo := &fakeRepository{storedAdmins: []entity.Admin{activeAdmin(t, panelPassword, entity.RoleOwner)}}
	clock := testClock
	auth := newAuth(repo, &fakeLimiter{deny: map[string]bool{}}, clock)

	issued, err := auth.Login(context.Background(), &dto.LoginRequest{
		Email: "owner@example.test", Password: panelPassword,
	}, "203.0.113.10")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	identity, err := auth.Session(context.Background(), issued.SessionToken)
	if err != nil {
		t.Fatalf("live session: %v", err)
	}
	if identity.AdminID != 7 || identity.Email != "owner@example.test" {
		t.Fatalf("identity = %+v, want the signed-in admin", identity)
	}

	// One second before the TTL it still works; one after it does not.
	clock.now = clock.now.Add(12*time.Hour - time.Second)
	if _, err := newAuth(repo, &fakeLimiter{deny: map[string]bool{}}, clock).
		Session(context.Background(), issued.SessionToken); err != nil {
		t.Fatalf("session just before the TTL: %v", err)
	}

	clock.now = clock.now.Add(2 * time.Second)
	if _, err := newAuth(repo, &fakeLimiter{deny: map[string]bool{}}, clock).
		Session(context.Background(), issued.SessionToken); !errors.Is(err, service.ErrUnauthenticated) {
		t.Fatalf("expired session error = %v, want ErrUnauthenticated", err)
	}

	// Revocation is a server-side fact: the row is marked, and a live TTL does not
	// resurrect it.
	clock.now = testClock.Now()
	if err := auth.Logout(context.Background(), issued.SessionToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if len(repo.revokedHashes) != 1 || repo.revokedHashes[0] != securetoken.Hash(issued.SessionToken) {
		t.Fatalf("revoked hashes = %v, want the session's hash", repo.revokedHashes)
	}
	if _, err := auth.Session(context.Background(), issued.SessionToken); !errors.Is(err, service.ErrUnauthenticated) {
		t.Fatalf("revoked session error = %v, want ErrUnauthenticated", err)
	}
}

// TestChangeOwnPasswordRequiresTheCurrentPasswordAndTheRule pins both halves of
// the self-service path: a stolen cookie cannot lock the operator out, and a weak
// replacement is refused with the same rule the bootstrap uses.
func TestChangeOwnPasswordRequiresTheCurrentPasswordAndTheRule(t *testing.T) {
	repo := &fakeRepository{storedAdmins: []entity.Admin{activeAdmin(t, panelPassword, entity.RoleOwner)}}
	auth := newAuth(repo, &fakeLimiter{deny: map[string]bool{}}, testClock)
	actor := audit.Actor{ID: 7, Email: "owner@example.test", IP: "203.0.113.11"}

	if err := auth.ChangeOwnPassword(context.Background(), actor, &dto.ChangePasswordRequest{
		CurrentPassword: "bukan-sandi-yang-benar",
		NewPassword:     "kata-sandi-baru-yang-panjang",
	}); !errors.Is(err, service.ErrCurrentPasswordInvalid) {
		t.Fatalf("wrong current password error = %v, want ErrCurrentPasswordInvalid", err)
	}
	if err := auth.ChangeOwnPassword(context.Background(), actor, &dto.ChangePasswordRequest{
		CurrentPassword: panelPassword,
		NewPassword:     "pendek",
	}); !errors.Is(err, service.ErrPasswordWeak) {
		t.Fatalf("short password error = %v, want ErrPasswordWeak", err)
	}
	if err := auth.ChangeOwnPassword(context.Background(), actor, &dto.ChangePasswordRequest{
		CurrentPassword: panelPassword,
		NewPassword:     "owner@example.test",
	}); !errors.Is(err, service.ErrPasswordWeak) {
		t.Fatalf("password-equals-email error = %v, want ErrPasswordWeak", err)
	}
	if len(repo.passwordHashes) != 0 {
		t.Fatal("a refused change wrote a hash")
	}

	if err := auth.ChangeOwnPassword(context.Background(), actor, &dto.ChangePasswordRequest{
		CurrentPassword: panelPassword,
		NewPassword:     "kata-sandi-baru-yang-panjang",
	}); err != nil {
		t.Fatalf("change own password: %v", err)
	}
	if len(repo.passwordHashes) != 1 {
		t.Fatalf("hashes written = %d, want 1", len(repo.passwordHashes))
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(repo.passwordHashes[0]),
		[]byte("kata-sandi-baru-yang-panjang"),
	); err != nil {
		t.Fatalf("the stored hash does not verify the new password: %v", err)
	}
}
