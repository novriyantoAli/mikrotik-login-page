package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	authdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/dto"
	authservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
)

// sessionContextKey is where Require stores the identity for the handler. It is
// private so no handler can invent its own copy of the session.
const sessionContextKey = "session"

// SessionAuthenticator is the narrow slice of the auth service this middleware
// needs: resolving a cookie value into an identity. The auth service satisfies
// it, and a test supplies a two-method fake instead of the whole domain.
type SessionAuthenticator interface {
	Authenticate(ctx context.Context, sessionToken string) (*authdto.SessionResponse, error)
}

// SessionSettings are the cookie knobs shared with the auth handler, provided
// from deployment configuration.
type SessionSettings struct {
	// CookieName is the opaque session cookie (ADR 0009/0011).
	CookieName string
	// Secure mirrors the cookie attribute so an expired cookie is cleared with
	// the same attributes it was set with.
	Secure bool
}

// Session is the authenticated identity every user endpoint works with. It
// carries the canonical Phone Number, which is the only customer identity this
// system stores (CONTEXT.md).
type Session struct {
	PhoneNumber string
}

// SessionGuard authenticates requests for the user-facing endpoints (#14, #15,
// #16 all need it). It never reads the sessions table itself: authorisation is
// the auth service's job (golden rule: layers only talk to the layer below).
type SessionGuard struct {
	auth     SessionAuthenticator
	settings SessionSettings
	logger   *zap.Logger
}

// NewSessionGuard builds the guard; Fx injects the auth service and the cookie
// settings.
func NewSessionGuard(
	auth SessionAuthenticator,
	settings SessionSettings,
	logger *zap.Logger,
) *SessionGuard {
	return &SessionGuard{auth: auth, settings: settings, logger: logger}
}

// Require rejects a request without a live session. A stale cookie is cleared so
// the browser stops sending it, and an unknown session answers 401 exactly like
// a missing one: there is nothing useful to distinguish (PRD point 12).
func (g *SessionGuard) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := httpx.ReadSessionCookie(c, g.settings.CookieName)
		if !ok {
			unauthorized(c)
			return
		}

		identity, err := g.auth.Authenticate(c.Request.Context(), token)
		switch {
		case errors.Is(err, authservice.ErrUnauthenticated):
			httpx.ClearSessionCookie(c, g.settings.CookieName, g.settings.Secure)
			unauthorized(c)
			return
		case err != nil:
			g.logger.Error("authenticate session", zap.Error(err))
			httpx.Internal(c)
			return
		}

		StashSession(c, Session{PhoneNumber: identity.PhoneNumber})
		c.Next()
	}
}

// StashSession stores the identity for the handlers behind Require. It is
// exported so handler tests can drive a request without a cookie jar, and it is
// the only way the identity enters a request context.
func StashSession(c *gin.Context, session Session) {
	c.Set(sessionContextKey, session)
}

// SessionFrom returns the identity Require stored, or ok=false when the route is
// not protected by it.
func SessionFrom(c *gin.Context) (Session, bool) {
	value, ok := c.Get(sessionContextKey)
	if !ok {
		return Session{}, false
	}
	session, ok := value.(Session)
	return session, ok
}

// RequireOwnPhoneNumber answers 404 when the Phone Number named in a request is
// not the session's. Mismatches are deliberately "not found" instead of
// "forbidden", so an endpoint can never confirm that another buyer's data exists
// (PRD point 13). It returns true when the request may continue.
//
// The candidate is normalized with the one shared rule (PRD point 9) before the
// comparison: a buyer who retypes their own number as "0812…" is still that
// buyer, and an unparseable value is a mismatch rather than a 400.
func RequireOwnPhoneNumber(c *gin.Context, phoneNumber string) bool {
	session, ok := SessionFrom(c)
	if !ok {
		unauthorized(c)
		return false
	}
	if strings.TrimSpace(phoneNumber) == "" {
		return true
	}

	canonical, err := phone.Normalize(phoneNumber)
	if err != nil || canonical != session.PhoneNumber {
		httpx.Error(c, http.StatusNotFound, httpx.CodeNotFound, "Data tidak ditemukan.")
		return false
	}
	return true
}

func unauthorized(c *gin.Context) {
	httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized,
		"Sesi tidak ditemukan atau sudah berakhir. Minta tautan masuk baru.")
}
