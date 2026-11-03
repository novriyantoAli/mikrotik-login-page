package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	adminservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// adminSessionKey is where AdminGuard stores the resolved identity. It is
// deliberately not the buyer's "session" key: one bug in a lookup must not let a
// buyer session be read as an admin one (ADR 0024 §2).
const adminSessionKey = "admin_session"

// AdminCookieSettings are the transport knobs for the admin cookie, provided from
// deployment configuration. They are separate from the buyer's on purpose: the
// name, the path and the lifetime must never be inherited (ADR 0024 §2).
type AdminCookieSettings struct {
	Name   string
	Secure bool
	TTL    time.Duration
}

// AdminSessionResolver is the narrow slice of the admin auth service this guard
// needs: resolving a cookie value into an identity. The auth service satisfies it,
// and a test supplies a one-method fake instead of the whole domain.
type AdminSessionResolver interface {
	Session(ctx context.Context, sessionToken string) (*dto.SessionResponse, error)
}

// AdminGuard authenticates every `/api/admin/**` request and enforces ADR 0024 §3's
// role matrix one route at a time. It never reads admin_sessions itself:
// authorisation is the admin auth service's job (golden rule 2).
type AdminGuard struct {
	auth    AdminSessionResolver
	cookies AdminCookieSettings
	logger  *zap.Logger
}

// NewAdminGuard builds the guard; Fx injects the admin auth service and the admin
// cookie settings.
func NewAdminGuard(auth AdminSessionResolver, cookies AdminCookieSettings, logger *zap.Logger) *AdminGuard {
	return &AdminGuard{auth: auth, cookies: cookies, logger: logger}
}

// Require rejects a request without a live admin session, then rejects a role that
// is not in the list. No roles means "any authenticated admin": 401 is only ever
// "no session", and 403 is the answer for a role that may not act (ADR 0024 §3).
func (g *AdminGuard) Require(roles ...entity.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := g.resolve(c)
		if !ok {
			return
		}

		if !adminRoleAllowed(identity.Role, roles) {
			// The identity is not stashed on a refused request: nothing behind this
			// point may act on a role the matrix just rejected.
			httpx.Error(c, http.StatusForbidden, httpx.CodeForbidden,
				"Peran Anda tidak diizinkan untuk tindakan ini.")
			return
		}

		StashAdminIdentity(c, identity)
		c.Next()
	}
}

// resolve reads the admin cookie and asks the auth service about it. A stale cookie
// is cleared with the same attributes it was set with, so the browser stops
// sending it; anything else is a server failure.
func (g *AdminGuard) resolve(c *gin.Context) (*dto.SessionResponse, bool) {
	token, ok := httpx.ReadSessionCookie(c, g.cookies.Name)
	if !ok {
		adminUnauthorized(c)
		return nil, false
	}

	identity, err := g.auth.Session(c.Request.Context(), token)
	switch {
	case errors.Is(err, adminservice.ErrUnauthenticated):
		httpx.ClearAdminSessionCookie(c, g.cookies.Name, g.cookies.Secure)
		adminUnauthorized(c)
		return nil, false
	case err != nil:
		g.logger.Error("authenticate admin session", zap.Error(err))
		httpx.Internal(c)
		return nil, false
	}
	return identity, true
}

// adminRoleAllowed reports whether the granted role is one of the accepted ones. An
// empty list accepts every role the matrix knows, so a route that needs no
// particular role says so by naming none.
func adminRoleAllowed(granted string, accepted []entity.Role) bool {
	if len(accepted) == 0 {
		return true
	}
	for _, role := range accepted {
		if entity.Role(granted) == role {
			return true
		}
	}
	return false
}

// StashAdminIdentity stores the identity for the handlers behind Require. It is
// exported so handler tests can drive a request without a cookie jar, and it is the
// only way the identity enters a request context.
func StashAdminIdentity(c *gin.Context, identity *dto.SessionResponse) {
	c.Set(adminSessionKey, identity)
}

// AdminIdentityFrom returns the identity Require stored, or ok=false when the route
// is not protected by it.
func AdminIdentityFrom(c *gin.Context) (*dto.SessionResponse, bool) {
	value, ok := c.Get(adminSessionKey)
	if !ok {
		return nil, false
	}
	identity, ok := value.(*dto.SessionResponse)
	return identity, ok
}

// AdminActorFrom builds the audit actor from the session and the request's address.
// The actor is never taken from a body: a trail row must name who really acted
// (ADR 0027 §3).
func AdminActorFrom(c *gin.Context) (audit.Actor, bool) {
	identity, ok := AdminIdentityFrom(c)
	if !ok {
		return audit.Actor{}, false
	}
	return audit.Actor{ID: identity.AdminID, Email: identity.Email, IP: c.ClientIP()}, true
}

func adminUnauthorized(c *gin.Context) {
	httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized,
		"Sesi admin tidak ditemukan atau sudah berakhir. Silakan masuk kembali.")
}
