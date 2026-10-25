// Package handler adapts the health service to HTTP. It holds no business
// logic: it calls the service and maps the result onto the shared envelope.
package handler

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// Handler serves the health endpoint.
type Handler struct {
	service service.Service
	logger  *zap.Logger
}

// NewHandler builds the handler; Fx injects the service.
func NewHandler(healthService service.Service, logger *zap.Logger) *Handler {
	return &Handler{service: healthService, logger: logger}
}

// RegisterRoutes mounts the health endpoint on the API group.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup) {
	api.GET("/health", h.GetHealth)
}

// GetHealth reports this API and each dependency. It answers 200 when every
// dependency is reachable and 503 when any is not, so a status page can render
// partial outages instead of a blank failure.
//
//	@Summary		Health of the backend and every dependency
//	@Description	Returns the status of the backend, Postgres, MySQL FreeRADIUS and Redis.
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	dto.Health	"every dependency is up"
//	@Failure		503	{object}	dto.Health	"at least one dependency is down"
//	@Router			/health [get]
func (h *Handler) GetHealth(c *gin.Context) {
	report := h.service.Report(c.Request.Context())

	httpx.Data(c, report.StatusCode(), report)
}
