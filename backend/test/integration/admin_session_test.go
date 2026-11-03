package integration

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	adminrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/securetoken"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// adminRepository is the panel repository against the harness's real schema, with
// no service in the way, so the statements ADR 0024 §2 describes are asserted
// directly.
func adminRepository(harness *testutil.APIHarness) adminrepository.Repository {
	return adminrepository.NewRepository(
		harness.Postgres,
		adminrepository.NewAuditRecorder(zap.NewNop()),
		zap.NewNop(),
	)
}

// TestAdminSessionLookupFiltersExpiredAndRevoked is the repository half of the
// session rules: only the hash is stored, and a dead row answers nil rather than a
// stale identity.
func TestAdminSessionLookupFiltersExpiredAndRevoked(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()
	repo := adminRepository(harness)
	owner := readOwner(t, harness)

	now := time.Date(2026, time.November, 3, 10, 0, 0, 0, time.UTC)
	raw, hash, err := securetoken.New()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	session := &entity.Session{TokenHash: hash, AdminID: owner.ID, ExpiresAt: now.Add(12 * time.Hour)}
	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var stored entity.Session
	if err := harness.Postgres.Where("id = ?", session.ID).First(&stored).Error; err != nil {
		t.Fatalf("read stored session: %v", err)
	}
	if stored.TokenHash == raw {
		t.Fatal("the raw token was stored; only its hash may be")
	}
	if stored.TokenHash != securetoken.Hash(raw) {
		t.Fatal("the stored value is not the hash of the token")
	}

	live, err := repo.Session(ctx, hash, now)
	if err != nil {
		t.Fatalf("read live session: %v", err)
	}
	if live == nil || live.Admin.ID != owner.ID || live.Admin.Role != entity.RoleOwner {
		t.Fatalf("live session = %+v, want the seeded owner", live)
	}

	expired, err := repo.Session(ctx, hash, now.Add(13*time.Hour))
	if err != nil {
		t.Fatalf("read expired session: %v", err)
	}
	if expired != nil {
		t.Fatal("an expired session still resolves")
	}

	if err := repo.RevokeSession(ctx, hash, now.Add(time.Minute)); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	revoked, err := repo.Session(ctx, hash, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("read revoked session: %v", err)
	}
	if revoked != nil {
		t.Fatal("a revoked session still resolves")
	}

	// An unknown hash is not an error: sign-out and lookups are idempotent.
	if missing, err := repo.Session(ctx, securetoken.Hash("never-issued"), now); err != nil || missing != nil {
		t.Fatalf("unknown session = %+v, %v, want nil, nil", missing, err)
	}
}

// TestRevokeAllForAdminTouchesOnlyThatAdmin is ADR 0024 §5's blast radius: a reset
// or a deactivation must end every session of one admin and no other.
func TestRevokeAllForAdminTouchesOnlyThatAdmin(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	ctx := context.Background()
	repo := adminRepository(harness)
	owner := readOwner(t, harness)

	support := &entity.Admin{
		Email:        "support-sessions@example.test",
		PasswordHash: "$2a$12$zIz7LKJC5RtCYAo1hNbpP.gCq2iyh8.zfUvhmlhzsScrjFx7qUBQS",
		Role:         entity.RoleSupport,
		IsActive:     true,
	}
	actor := audit.Actor{ID: owner.ID, Email: owner.Email}
	entry := audit.Entry{Actor: actor, Action: audit.ActionAdminCreate}
	if err := repo.CreateAdmin(ctx, support, entry); err != nil {
		t.Fatalf("create support: %v", err)
	}

	now := time.Date(2026, time.November, 3, 11, 0, 0, 0, time.UTC)
	mint := func(adminID uint) string {
		t.Helper()
		_, hash, err := securetoken.New()
		if err != nil {
			t.Fatalf("mint token: %v", err)
		}
		if err := repo.CreateSession(ctx, &entity.Session{
			TokenHash: hash, AdminID: adminID, ExpiresAt: now.Add(12 * time.Hour),
		}); err != nil {
			t.Fatalf("create session: %v", err)
		}
		return hash
	}
	// Two sessions for the owner, because "all" is the part that can be got wrong.
	ownerHashes := []string{mint(owner.ID), mint(owner.ID)}
	supportHash := mint(support.ID)

	if err := repo.RevokeAllForAdmin(ctx, owner.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("revoke all for the owner: %v", err)
	}

	for _, hash := range ownerHashes {
		live, err := repo.Session(ctx, hash, now.Add(2*time.Minute))
		if err != nil {
			t.Fatalf("read owner session: %v", err)
		}
		if live != nil {
			t.Fatalf("an owner session survived revoke-all: %+v", live)
		}
	}

	liveSupport, err := repo.Session(ctx, supportHash, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("read support session: %v", err)
	}
	if liveSupport == nil || liveSupport.Admin.ID != support.ID {
		t.Fatal("revoke-all for the owner ended the support admin's session too")
	}
}
