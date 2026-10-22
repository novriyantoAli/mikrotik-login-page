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
