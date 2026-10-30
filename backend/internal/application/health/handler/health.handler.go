// Package handler adapts the health service to HTTP. It holds no business
// logic: it calls the service and maps the result onto the shared envelope.
package handler

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	healthdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// Health is the health report this handler answers with. It is an alias, not a
// second shape: the DTO owns the fields and the service returns it, so this file
// never constructs one. The alias exists because swag resolves an annotation's
// type names against the annotated file's own imports, and a handler that only
// forwards service output imports none of them.
type Health = healthdto.Health

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
//	@Success		200	{object}	Health	"every dependency is up"
//	@Failure		503	{object}	Health	"at least one dependency is down"
//	@Router			/health [get]
func (h *Handler) GetHealth(c *gin.Context) {
	report := h.service.Report(c.Request.Context())

	httpx.Data(c, report.StatusCode(), report)
}
