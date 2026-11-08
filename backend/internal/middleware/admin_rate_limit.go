// This file bounds how often one operator may open a buyer's PIN. The budget is the
// admin's, not the address's: two support staff behind one office IP must not spend
// each other's allowance, and the same admin cannot dodge the bound by moving
// networks (ADR 0027 §4, PRD admin-panel slice 2).
package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/ratelimit"
)

// PINRevealLimitSettings is the one window a reveal budget has, provided from
// deployment configuration (`admin.pin_reveal`). One window is enough because there
// is one dimension: the signed-in admin.
type PINRevealLimitSettings struct {
	Window time.Duration
	Limit  int
}

// PINRevealLimiter counts PIN reveals per admin id in Redis. It lives in the
// transport layer, unlike the sign-in budget, because the reveal's rules belong to
// the transaction domain while its defense belongs to the panel's HTTP edge: the
// same reason the cookie name and session lifetime are configured here.
type PINRevealLimiter struct {
	limiter  ratelimit.Limiter
	settings PINRevealLimitSettings
	logger   *zap.Logger
}

// NewPINRevealLimiter builds the limiter; Fx injects the Redis-backed counter and the
// configured window.
func NewPINRevealLimiter(
	limiter ratelimit.Limiter,
	settings PINRevealLimitSettings,
	logger *zap.Logger,
) *PINRevealLimiter {
	return &PINRevealLimiter{limiter: limiter, settings: settings, logger: logger}
}

// Limit is mounted after AdminGuard.Require, so the identity is already resolved. A
// request without one is refused rather than counted under a shared key: a budget
// nobody owns is a budget nobody is held to.
func (l *PINRevealLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := AdminIdentityFrom(c)
		if !ok || identity == nil {
			adminUnauthorized(c)
			return
		}

		key := fmt.Sprintf("admin:pin_reveal:admin:%d", identity.AdminID)
		allowed, err := l.limiter.Allow(c.Request.Context(), key, l.settings.Limit, l.settings.Window)
		if err != nil {
			// An unreachable Redis must not remove the bound that exists to keep a
			// stolen session from harvesting PINs at leisure (ADR 0024 §4's rule for
			// the sign-in budget, applied to the reveal).
			l.logger.Error("admin pin reveal rate limit",
				zap.Uint("admin_id", identity.AdminID), zap.Error(err))
			httpx.Internal(c)
			return
		}
		if !allowed {
			httpx.Error(c, http.StatusTooManyRequests, httpx.CodeRateLimited,
				"Terlalu banyak permintaan membuka PIN dalam waktu singkat. Coba lagi nanti.")
			return
		}

		c.Next()
	}
}
