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
