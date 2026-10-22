// Package handler adapts the Active Period read to HTTP: query parameter in,
// shared envelope out, stable error codes for the two failure categories.
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// Handler serves the public Active Period endpoint. It holds no logger: the
// FreeRADIUS adapter logs the driver error where it is first observed.
type Handler struct {
	service service.Service
}

// NewHandler builds the handler; Fx injects the service.
func NewHandler(activePeriod service.Service) *Handler {
	return &Handler{service: activePeriod}
}

// RegisterRoutes mounts the Active Period endpoint on the API group.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup) {
	api.GET("/active-period", h.GetActivePeriod)
}

// GetActivePeriod answers the status page's Active Period read (ADR 0005).
// It is public and read-only. A Phone Number with no expiration is a 200 with a
// null expiry; an unreachable FreeRADIUS MySQL is a 503, so the page can show
// "tidak dapat dimuat" instead of a wrong answer.
//
//	@Summary		Public Active Period for the Login Page status page
//	@Description	Returns the end of the Active Period for a Phone Number, or null when it has none.
//	@Tags			active-period
//	@Produce		json
//	@Param			phone	query		string	true	"Phone Number (08…, +62… or 628…)"
//	@Success		200		{object}	httpx.Envelope{data=dto.ActivePeriodResponse}	"expiry is an ISO-8601 timestamp or null"
//	@Failure		400		{object}	httpx.ErrorResponse								"the Phone Number is missing or malformed"
//	@Failure		503		{object}	httpx.ErrorResponse								"FreeRADIUS MySQL is unreachable"
//	@Router			/active-period [get]
func (h *Handler) GetActivePeriod(c *gin.Context) {
	activePeriod, err := h.service.GetActivePeriod(c.Request.Context(), c.Query("phone"))
	switch {
	case errors.Is(err, service.ErrInvalidPhoneNumber):
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Phone Number tidak valid.")
		return
	case err != nil:
		// The adapter logged the driver error; the public message stays generic.
		httpx.Error(c, http.StatusServiceUnavailable, httpx.CodeDependencyFailed, "Active Period sedang tidak tersedia.")
		return
	}

	if activePeriod == nil {
		// The frozen contract always carries a data object, never {"data":null}.
		activePeriod = &dto.ActivePeriodResponse{}
	}

	httpx.Data(c, http.StatusOK, activePeriod)
}
