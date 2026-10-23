// Package httpx writes the one HTTP envelope every endpoint uses, so the
// purchase app never has to special-case a handler: {"data": ...} on success and
// {"error": {"code", "message"}} on failure (PRD point 31).
package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Envelope is the success body. Extra keys (pagination, counts) may be added
// alongside Data by endpoints that need them.
type Envelope struct {
	Data any `json:"data"`
}

// ErrorBody is the failure payload.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the failure envelope.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// Stable error codes clients branch on. Add codes here rather than in handlers.
const (
	CodeInternal         = "internal_error"
	CodeInvalidRequest   = "invalid_request"
	CodeNotFound         = "not_found"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeConflict         = "conflict"
	CodeRateLimited      = "rate_limited"
	CodeDependencyFailed = "dependency_unavailable"
	// CodeMagicLinkInvalid is the one answer for an unknown, expired or already
	// used Magic Link, so a caller cannot tell them apart (ADR 0011).
	CodeMagicLinkInvalid = "magic_link_invalid"
)

// Data writes a success envelope.
func Data(c *gin.Context, status int, data any) {
	c.JSON(status, Envelope{Data: data})
}

// Error writes a failure envelope with a stable code the client can branch on.
func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, ErrorResponse{Error: ErrorBody{Code: code, Message: message}})
}

// Internal reports an unexpected failure without leaking its details to the
// caller; the message is generic and the details belong in the log.
func Internal(c *gin.Context) {
	Error(c, http.StatusInternalServerError, CodeInternal, "Terjadi kesalahan pada server.")
}

// SetSessionCookie writes the opaque session cookie (ADR 0009/0011). The value is
// a random token, never a JWT, and JavaScript can never read it.
func SetSessionCookie(c *gin.Context, name, value string, maxAgeSeconds int, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, value, maxAgeSeconds, "/", "", secure, true)
}

// ReadSessionCookie returns the cookie value, or ok=false when the browser sent
// none. An empty cookie is treated as absent.
func ReadSessionCookie(c *gin.Context, name string) (string, bool) {
	value, err := c.Cookie(name)
	if err != nil || value == "" {
		return "", false
	}
	return value, true
}

// ClearSessionCookie removes the cookie. It must be paired with a server-side
// revocation: the cookie alone is not the session (ADR 0011).
func ClearSessionCookie(c *gin.Context, name string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, "", -1, "/", "", secure, true)
}
