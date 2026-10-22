// Package handler adapts the Package catalog to HTTP. It holds no business
// logic: it calls the service and writes the shared success envelope.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// Handler serves the public Package catalog. It holds no logger: the
// repository logs an error where it is first observed, so logging it again here
// would duplicate the same line.
type Handler struct {
	service service.Service
}

// NewHandler builds the handler; Fx injects the service.
func NewHandler(catalog service.Service) *Handler {
	return &Handler{service: catalog}
}

// RegisterRoutes mounts the catalog endpoint on the API group.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup) {
	api.GET("/packages", h.GetPackages)
}

// GetPackages lists every Package the hotspot sells. The response shape is
// frozen by the Login Page client seam (PRD point 8): it is public, read-only
// and needs no session.
//
//	@Summary		Public Package catalog for the Login Page
//	@Description	Returns every Package with the display fields the Login Page renders.
//	@Tags			packages
//	@Produce		json
//	@Success		200	{object}	httpx.Envelope{data=[]dto.PackageResponse}	"the catalog, possibly empty"
//	@Failure		500	{object}	httpx.ErrorResponse							"the catalog could not be read"
//	@Router			/packages [get]
func (h *Handler) GetPackages(c *gin.Context) {
	packages, err := h.service.ListPackages(c.Request.Context())
	if err != nil {
		httpx.Internal(c)
		return
	}
	if packages == nil {
		// The frozen contract says "array", never null, even for an empty
		// catalog; keep that true here rather than in every client.
		packages = []dto.PackageResponse{}
	}

	httpx.Data(c, http.StatusOK, packages)
}
