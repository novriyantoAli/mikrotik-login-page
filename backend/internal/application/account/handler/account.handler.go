// Package handler adapts Account Status to HTTP. It holds no business logic: the
// session decides whose account is read, the service composes the answer, and the
// shared envelope carries it (golden rule 2).
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// Handler serves the Account Status reads.
type Handler struct {
	service service.Service
	guard   *middleware.SessionGuard
}

// NewHandler builds the handler; Fx injects the service and the session guard.
func NewHandler(accountService service.Service, guard *middleware.SessionGuard) *Handler {
	return &Handler{service: accountService, guard: guard}
}

// RegisterRoutes mounts both reads behind the session guard. The Phone Number is
// never taken from the request: it is the session's, and an optional query value
// is only a cross-check answered as not found (PRD point 13).
func (h *Handler) RegisterRoutes(api *gin.RouterGroup) {
	protected := api.Group("", h.guard.Require())
	{
		protected.GET("/account", h.Summary)
		protected.GET("/account/extend-preview", h.ExtendPreview)
	}
}

// Summary answers the Account Status page with the active Package, the remaining
// Active Period, and the current PIN.
//
//	@Summary		Account Status summary
//	@Description	Active Package, Active Period state and the current PIN for the session's Phone Number.
//	@Tags			account
//	@Produce		json
//	@Param			phone_number	query		string	false	"Optional cross-check; a foreign number answers 404"
//	@Success		200				{object}	httpx.Envelope{data=dto.AccountResponse}
//	@Failure		401				{object}	httpx.ErrorResponse	"no live session"
//	@Failure		404				{object}	httpx.ErrorResponse	"the Phone Number in the query is not the session's"
//	@Router			/account [get]
func (h *Handler) Summary(c *gin.Context) {
	session, ok := middleware.SessionFrom(c)
	if !ok {
		httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Sesi tidak ditemukan.")
		return
	}

	if !middleware.RequireOwnPhoneNumber(c, c.Query("phone_number")) {
		return
	}

	account, err := h.service.Summary(c.Request.Context(), session.PhoneNumber)
	if err != nil {
		writeError(c, err)
		return
	}

	httpx.Data(c, http.StatusOK, account)
}

// ExtendPreview estimates where buying a Package leaves the Active Period, so the
// checkout can confirm an Extend before the buyer pays.
//
//	@Summary		Extend preview
//	@Description	Estimated expiration after buying the Package, computed with the rule provisioning applies.
//	@Tags			account
//	@Produce		json
//	@Param			package_id		query		int		true	"Package the buyer is about to purchase"
//	@Param			phone_number	query		string	false	"Optional cross-check; a foreign number answers 404"
//	@Success		200				{object}	httpx.Envelope{data=dto.ExtendPreviewResponse}
//	@Failure		400				{object}	httpx.ErrorResponse	"missing or unknown Package"
//	@Failure		401				{object}	httpx.ErrorResponse	"no live session"
//	@Failure		404				{object}	httpx.ErrorResponse	"the Phone Number in the query is not the session's"
//	@Router			/account/extend-preview [get]
func (h *Handler) ExtendPreview(c *gin.Context) {
	session, ok := middleware.SessionFrom(c)
	if !ok {
		httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Sesi tidak ditemukan.")
		return
	}

	if !middleware.RequireOwnPhoneNumber(c, c.Query("phone_number")) {
		return
	}

	packageID, err := strconv.ParseUint(c.Query("package_id"), 10, 64)
	if err != nil || packageID == 0 {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Package wajib dipilih.")
		return
	}

	preview, err := h.service.ExtendPreview(c.Request.Context(), session.PhoneNumber, uint(packageID))
	if err != nil {
		writeError(c, err)
		return
	}

	httpx.Data(c, http.StatusOK, preview)
}

// writeError maps the domain's sentinels onto HTTP; anything else stays a generic
// 500 so no internal detail reaches a buyer.
func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrPackageUnknown):
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Package tidak dikenal.")
	default:
		httpx.Internal(c)
	}
}
