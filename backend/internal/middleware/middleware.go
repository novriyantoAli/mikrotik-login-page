// Package middleware holds the HTTP middlewares every route shares.
package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// RequestLogger writes one structured line per request so an operator can trace
// a buyer's reported problem without grepping unstructured output.
func RequestLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		logger.Info("http request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(started)),
			zap.String("client_ip", c.ClientIP()),
		)
	}
}

// CORS lets the Login Page read the two public endpoints. RouterOS serves that
// page from a per-hotspot origin the backend cannot know in advance, so the
// allowlist would have to be every router address; the reads are public and
// carry no session, so a wildcard origin is equivalent to what anyone can do
// with curl.
//
// Only safe methods get the header, and credentials are never allowed: a
// cross-origin page can read the catalog and an Active Period, but it cannot
// read a user-specific response and cannot preflight a state-changing call.
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead:
			c.Header("Access-Control-Allow-Origin", "*")
		case http.MethodOptions:
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// Recovery turns a panic into the standard error envelope and logs the stack.
func Recovery(logger *zap.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		logger.Error("panic recovered",
			zap.Any("panic", recovered),
			zap.String("path", c.Request.URL.Path),
		)
		httpx.Error(c, http.StatusInternalServerError, httpx.CodeInternal, "Terjadi kesalahan pada server.")
	})
}
