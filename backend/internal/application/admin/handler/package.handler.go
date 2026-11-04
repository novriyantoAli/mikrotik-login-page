// Package handler adapts the panel's catalog routes to HTTP. The catalog's rules
// stay in the packages domain; this file only resolves the admin, enforces the role
// matrix and maps the service's refusals onto the shared envelope (golden rule 2).
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	packagesdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	packageservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// PackageHandler serves `/api/admin/packages`. It depends on the packages domain's
// Service interface, never on its repository: cross-domain calls go through the
// owning domain's public surface (golden rule 5).
type PackageHandler struct {
	catalog packageservice.Service
	guard   *middleware.AdminGuard
}

// NewPackageHandler builds the handler; Fx injects the catalog service and the
// admin guard.
func NewPackageHandler(catalog packageservice.Service, guard *middleware.AdminGuard) *PackageHandler {
	return &PackageHandler{catalog: catalog, guard: guard}
}

// RegisterRoutes mounts the panel's catalog reads and writes. Every role may read
// the catalog (retired Packages included); only an owner may write it
// (ADR 0024 §3).
func (h *PackageHandler) RegisterRoutes(api *gin.RouterGroup) {
	admin := api.Group("/admin")

	catalog := admin.Group("/packages", h.guard.Require())
	{
		catalog.GET("", h.ListPackages)
	}

	owners := admin.Group("/packages", h.guard.Require(entity.RoleOwner))
	{
		owners.POST("", h.CreatePackage)
		owners.PUT("/:id", h.UpdatePackage)
		owners.POST("/:id/active", h.SetActivePackage)
	}
}

// ListPackages returns every Package, active or retired, in the panel's typed shape
// with each row's version.
//
//	@Summary		List the whole catalog
//	@Description	Every Package including retired ones, with the version an edit must present.
//	@Tags			admin-packages
//	@Produce		json
//	@Success		200	{object}	httpx.Envelope{data=[]packagesdto.AdminPackageResponse}
//	@Failure		401	{object}	httpx.ErrorResponse	"no live admin session"
//	@Router			/admin/packages [get]
func (h *PackageHandler) ListPackages(c *gin.Context) {
	packages, err := h.catalog.ListAdminPackages(c.Request.Context())
	if err != nil {
		httpx.Internal(c)
		return
	}
	if packages == nil {
		packages = []packagesdto.AdminPackageResponse{}
	}

	httpx.Data(c, http.StatusOK, packages)
}

// CreatePackage adds one Package to the catalog and leaves exactly one audit row
// for it.
//
//	@Summary		Create a Package
//	@Description	Validates every field in the service and writes one audit row in the same transaction. Owner only.
//	@Tags			admin-packages
//	@Accept			json
//	@Produce		json
//	@Param			request	body		packagesdto.CreatePackageRequest	true	"code, name, price, duration and rate limit"
//	@Success		201		{object}	httpx.Envelope{data=packagesdto.AdminPackageResponse}
//	@Failure		400		{object}	httpx.ErrorResponse	"malformed body"
//	@Failure		401		{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		403		{object}	httpx.ErrorResponse	"the session's role may not write the catalog"
//	@Failure		409		{object}	httpx.ErrorResponse	"the code is already in use"
//	@Failure		422		{object}	httpx.ErrorResponse	"a field breaks the catalog rules"
//	@Router			/admin/packages [post]
func (h *PackageHandler) CreatePackage(c *gin.Context) {
	actor, ok := middleware.AdminActorFrom(c)
	if !ok {
		adminUnauthorized(c)
		return
	}

	var request packagesdto.CreatePackageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	}

	created, err := h.catalog.CreatePackage(c.Request.Context(), actor, &request)
	if err != nil {
		writePackageError(c, err)
		return
	}

	httpx.Data(c, http.StatusCreated, created)
}

// UpdatePackage edits one Package the panel read at the submitted version.
//
//	@Summary		Edit a Package
//	@Description	Requires the version the panel read; a stale one answers 409 with the current row.
//	@Description	Code cannot be changed. Owner only.
//	@Tags			admin-packages
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int									true	"Package id"
//	@Param			request	body		packagesdto.UpdatePackageRequest	true	"new values plus the version read"
//	@Success		200		{object}	httpx.Envelope{data=packagesdto.AdminPackageResponse}
//	@Failure		400		{object}	httpx.ErrorResponse	"malformed body"
//	@Failure		401		{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		403		{object}	httpx.ErrorResponse	"the session's role may not write the catalog"
//	@Failure		404		{object}	httpx.ErrorResponse	"no such Package"
//	@Failure		409		{object}	packagesdto.VersionConflictResponse	"stale version; error.current is the row now"
//	@Failure		422		{object}	httpx.ErrorResponse	"an immutable code or a field that breaks the rules"
//	@Router			/admin/packages/{id} [put]
func (h *PackageHandler) UpdatePackage(c *gin.Context) {
	actor, ok := middleware.AdminActorFrom(c)
	if !ok {
		adminUnauthorized(c)
		return
	}
	id, ok := packageIDFrom(c)
	if !ok {
		return
	}

	var request packagesdto.UpdatePackageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	}

	updated, err := h.catalog.UpdatePackage(c.Request.Context(), actor, id, &request)
	if err != nil {
		writePackageError(c, err)
		return
	}

	httpx.Data(c, http.StatusOK, updated)
}

// SetActivePackage retires or revives one Package.
//
//	@Summary		Retire or revive a Package
//	@Description	Retiring the last active Package answers 409: the Login Page would have nothing to sell. Owner only.
//	@Tags			admin-packages
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int							true	"Package id"
//	@Param			request	body		packagesdto.SetActiveRequest	true	"the state to set"
//	@Success		200		{object}	httpx.Envelope{data=packagesdto.AdminPackageResponse}
//	@Failure		400		{object}	httpx.ErrorResponse	"malformed body"
//	@Failure		401		{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		403		{object}	httpx.ErrorResponse	"the session's role may not write the catalog"
//	@Failure		404		{object}	httpx.ErrorResponse	"no such Package"
//	@Failure		409		{object}	httpx.ErrorResponse	"the last active Package cannot be retired"
//	@Router			/admin/packages/{id}/active [post]
func (h *PackageHandler) SetActivePackage(c *gin.Context) {
	actor, ok := middleware.AdminActorFrom(c)
	if !ok {
		adminUnauthorized(c)
		return
	}
	id, ok := packageIDFrom(c)
	if !ok {
		return
	}

	var request packagesdto.SetActiveRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	}

	updated, err := h.catalog.SetActive(c.Request.Context(), actor, id, &request)
	if err != nil {
		writePackageError(c, err)
		return
	}

	httpx.Data(c, http.StatusOK, updated)
}

// packageIDFrom parses the route's Package id. An unknown or non-numeric id is a
// 404, never a 500: the panel should say "not found", not "server broke".
func packageIDFrom(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Error(c, http.StatusNotFound, httpx.CodeNotFound, "Package tidak ditemukan.")
		return 0, false
	}
	return uint(id), true
}

// writePackageError maps the catalog's refusals onto the panel's statuses. The
// messages are the service's operator-facing reasons; an unexpected failure stays
// a generic 500 so no internal text leaks.
func writePackageError(c *gin.Context, err error) {
	var fieldError *packageservice.FieldError
	var conflict *packageservice.VersionConflictError

	switch {
	case errors.As(err, &conflict):
		// The stale edit's 409 carries the row as it is now, so the panel can show
		// what changed instead of guessing (ADR 0025 §7).
		c.AbortWithStatusJSON(http.StatusConflict, packagesdto.VersionConflictResponse{
			Error: packagesdto.VersionConflictBody{
				Code:    httpx.CodeConflict,
				Message: "Paket sudah diubah orang lain. Muat ulang lalu ulangi.",
				Current: conflict.Current,
			},
		})
	case errors.Is(err, packageservice.ErrLastActivePackage):
		httpx.Error(c, http.StatusConflict, httpx.CodeConflict, "Package aktif terakhir tidak bisa dinonaktifkan.")
	case errors.Is(err, packageservice.ErrPackageCodeTaken):
		httpx.Error(c, http.StatusConflict, httpx.CodeConflict, "Code paket sudah dipakai.")
	case errors.Is(err, packageservice.ErrPackageCodeImmutable):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest,
			"Code paket tidak bisa diubah setelah dibuat.")
	case errors.As(err, &fieldError):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest, fieldError.Reason)
	case errors.Is(err, packageservice.ErrPackageInvalid):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest, "Data paket tidak valid.")
	case errors.Is(err, packageservice.ErrPackageNotFound):
		httpx.Error(c, http.StatusNotFound, httpx.CodeNotFound, "Package tidak ditemukan.")
	case errors.Is(err, packageservice.ErrActorRequired):
		// The guard rejects this before the handler; keep the answer honest if a
		// future route forgets it.
		adminUnauthorized(c)
	default:
		httpx.Internal(c)
	}
}
