package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// fakeLimiter records what a reveal budget was asked, so the test can assert the
// dimension as well as the answer.
type fakeLimiter struct {
	allowed bool
	err     error

	calls  int
	key    string
	limit  int
	window time.Duration
}

func (l *fakeLimiter) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	l.calls++
	l.key, l.limit, l.window = key, limit, window
	return l.allowed, l.err
}

// newRevealEngine mounts the limiter the way the panel does: behind the guard that
// resolved the session, on one route only.
func newRevealEngine(limiter *fakeLimiter, identity *dto.SessionResponse) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	guardLike := func(c *gin.Context) {
		if identity != nil {
			middleware.StashAdminIdentity(c, identity)
		}
		c.Next()
	}
	reveal := middleware.NewPINRevealLimiter(limiter,
		middleware.PINRevealLimitSettings{Window: 10 * time.Minute, Limit: 30},
		zap.NewNop())
	group := engine.Group("/api/admin", guardLike)
	group.POST("/transactions/:id/pin", reveal.Limit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return engine
}

func revealRequest(t *testing.T, engine *gin.Engine) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/api/admin/transactions/12/pin", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

// TestPINRevealLimiterCountsPerAdmin is the budget's dimension: one allowance per
// signed-in admin, at the configured window and limit — not one per address, which
// would let two operators share (ADR 0027 §4).
func TestPINRevealLimiterCountsPerAdmin(t *testing.T) {
	limiter := &fakeLimiter{allowed: true}
	engine := newRevealEngine(limiter, &dto.SessionResponse{AdminID: 7, Email: "support@example.test", Role: "support"})

	if response := revealRequest(t, engine); response.Code != http.StatusOK {
		t.Fatalf("reveal = %d, want 200", response.Code)
	}
	if limiter.key != "admin:pin_reveal:admin:7" {
		t.Fatalf("limit key = %q, want the admin's own counter", limiter.key)
	}
	if limiter.limit != 30 || limiter.window != 10*time.Minute {
		t.Fatalf("budget = %d/%v, want 30 per 10m", limiter.limit, limiter.window)
	}
}

// TestPINRevealLimiterAnswersARefusalWith429 keeps the panel branchable: a spent
// budget is the shared rate_limited code, not a bare 500.
func TestPINRevealLimiterAnswersARefusalWith429(t *testing.T) {
	engine := newRevealEngine(&fakeLimiter{allowed: false},
		&dto.SessionResponse{AdminID: 7, Email: "support@example.test", Role: "support"})

	response := revealRequest(t, engine)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("reveal = %d, want 429", response.Code)
	}

	var body httpx.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", response.Body.String(), err)
	}
	if body.Error.Code != httpx.CodeRateLimited {
		t.Fatalf("code = %q, want %q", body.Error.Code, httpx.CodeRateLimited)
	}
}

// TestPINRevealLimiterRefusesWhenTheCounterIsDown is the fail-closed rule: a
// limiter that cannot count must not wave the request through, because the bound is
// the only thing standing between a stolen session and a run of PINs.
func TestPINRevealLimiterRefusesWhenTheCounterIsDown(t *testing.T) {
	engine := newRevealEngine(&fakeLimiter{err: errors.New("redis is down")},
		&dto.SessionResponse{AdminID: 7, Email: "support@example.test", Role: "support"})

	response := revealRequest(t, engine)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("reveal = %d, want 500", response.Code)
	}

	var body httpx.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", response.Body.String(), err)
	}
	if body.Error.Code != httpx.CodeInternal {
		t.Fatalf("code = %q, want %q", body.Error.Code, httpx.CodeInternal)
	}
}

// TestPINRevealLimiterNeedsAnIdentity keeps the budget owned: mounted without the
// guard's identity the route refuses rather than counting every caller together.
func TestPINRevealLimiterNeedsAnIdentity(t *testing.T) {
	limiter := &fakeLimiter{allowed: true}
	engine := newRevealEngine(limiter, nil)

	if response := revealRequest(t, engine); response.Code != http.StatusUnauthorized {
		t.Fatalf("reveal = %d, want 401", response.Code)
	}
	if limiter.calls != 0 {
		t.Fatalf("the limiter was asked %d times without an identity", limiter.calls)
	}
}
