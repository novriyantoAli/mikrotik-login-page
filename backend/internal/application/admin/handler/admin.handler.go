// Package handler adapts the panel's sign-in and admin-management surface to
// HTTP. It holds no business logic: the guard resolves the admin session, the
// services enforce the rules, and the shared envelope carries the answer
// (golden rule 2).
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// Handler serves every `/api/admin/**` route this slice owns.
type Handler struct {
	auth    service.Auth
	admins  service.Service
	guard   *middleware.AdminGuard
	cookies middleware.AdminCookieSettings
}

// NewHandler builds the handler; Fx injects both services, the guard and the
// cookie settings.
func NewHandler(
	auth service.Auth,
	admins service.Service,
	guard *middleware.AdminGuard,
	cookies middleware.AdminCookieSettings,
) *Handler {
	return &Handler{auth: auth, admins: admins, guard: guard, cookies: cookies}
}

// RegisterRoutes mounts the panel's authentication and admin-management routes.
// The role each route requires is declared next to it, so ADR 0024 §3's matrix and
// this list are the same list in two places — the HTTP tests are what keep them
// honest.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup) {
	admin := api.Group("/admin")

	// Public by necessity: a login form cannot require a session. It is bounded by
	// the two rate limits instead (ADR 0024 §4).
	admin.POST("/auth/login", h.Login)

	authed := admin.Group("/auth", h.guard.Require())
	{
		authed.GET("/session", h.GetSession)
		authed.POST("/logout", h.Logout)
		authed.POST("/password", h.ChangePassword)
	}

	// Managing admins is owner-only: support and auditor never see the team
	// (ADR 0024 §3).
	owners := admin.Group("/admins", h.guard.Require(entity.RoleOwner))
	{
		owners.GET("", h.ListAdmins)
		owners.POST("", h.CreateAdmin)
		owners.POST("/:id/password", h.ResetPassword)
		owners.POST("/:id/active", h.SetActive)
	}
}

// Login signs an operator in. Unknown email, wrong password and a deactivated
// admin answer one status and one body, so the endpoint cannot be used to
// enumerate admins (ADR 0024 §4).
//
//	@Summary		Admin sign-in
//	@Description	Rate-limited per IP and per email. Unknown email and wrong password answer identically.
//	@Tags			admin-auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.LoginRequest	true	"email and password"
//	@Success		200		{object}	httpx.Envelope{data=dto.SessionResponse}
//	@Failure		400		{object}	httpx.ErrorResponse	"malformed body"
//	@Failure		401		{object}	httpx.ErrorResponse	"unknown email, wrong password or inactive admin"
//	@Failure		429		{object}	httpx.ErrorResponse	"login attempts exhausted for this address or email"
//	@Router			/admin/auth/login [post]
func (h *Handler) Login(c *gin.Context) {
	var request dto.LoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	}

	issued, err := h.auth.Login(c.Request.Context(), &request, c.ClientIP())
	switch {
	case errors.Is(err, service.ErrCredentialsInvalid):
		// One message for every credential failure: the body must not distinguish
		// "no such admin" from "wrong password" (ADR 0024 §4).
		httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized,
			"Email atau kata sandi salah.")
		return
	case errors.Is(err, service.ErrRateLimited):
		httpx.Error(c, http.StatusTooManyRequests, httpx.CodeRateLimited,
			"Terlalu banyak percobaan masuk. Coba lagi beberapa saat lagi.")
		return
	case err != nil:
		httpx.Internal(c)
		return
	}

	httpx.SetAdminSessionCookie(c, h.cookies.Name, issued.SessionToken,
		int(h.cookies.TTL.Seconds()), h.cookies.Secure)
	httpx.Data(c, http.StatusOK, issued.Response)
}

// GetSession reports the admin behind the cookie. The guard already resolved it,
// so this handler never reads the sessions table itself.
//
//	@Summary		Current admin session
//	@Description	Returns the email, role and expiry behind the admin cookie, or 401 when there is none.
//	@Tags			admin-auth
//	@Produce		json
//	@Success		200	{object}	httpx.Envelope{data=dto.SessionResponse}
//	@Failure		401	{object}	httpx.ErrorResponse	"no live admin session"
//	@Router			/admin/auth/session [get]
func (h *Handler) GetSession(c *gin.Context) {
	identity, ok := middleware.AdminIdentityFrom(c)
	if !ok {
		adminUnauthorized(c)
		return
	}
	httpx.Data(c, http.StatusOK, identity)
}

// Logout revokes the session and clears the admin cookie. Revocation is what makes
// the old cookie useless, not the deletion (ADR 0024 §5).
//
//	@Summary		Admin sign-out
//	@Description	Revokes the admin session row and clears the admin cookie.
//	@Tags			admin-auth
//	@Success		204
//	@Failure		401	{object}	httpx.ErrorResponse	"no live admin session"
//	@Router			/admin/auth/logout [post]
func (h *Handler) Logout(c *gin.Context) {
	if token, ok := httpx.ReadSessionCookie(c, h.cookies.Name); ok {
		if err := h.auth.Logout(c.Request.Context(), token); err != nil {
			httpx.Internal(c)
			return
		}
	}

	httpx.ClearAdminSessionCookie(c, h.cookies.Name, h.cookies.Secure)
	c.Status(http.StatusNoContent)
}

// ChangePassword replaces the signed-in admin's own password after re-checking the
// current one.
//
//	@Summary		Change own password
//	@Description	Replaces the signed-in admin's password. Under 12 characters or equal to the email is refused.
//	@Tags			admin-auth
//	@Accept			json
//	@Param			request	body		dto.ChangePasswordRequest	true	"current and new password"
//	@Success		204
//	@Failure		401	{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		422	{object}	httpx.ErrorResponse	"wrong current password or a password that breaks the rule"
//	@Router			/admin/auth/password [post]
func (h *Handler) ChangePassword(c *gin.Context) {
	actor, ok := middleware.AdminActorFrom(c)
	if !ok {
		adminUnauthorized(c)
		return
	}

	var request dto.ChangePasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	}

	err := h.auth.ChangeOwnPassword(c.Request.Context(), actor, &request)
	switch {
	case errors.Is(err, service.ErrCurrentPasswordInvalid):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest,
			"Kata sandi saat ini tidak benar.")
		return
	case errors.Is(err, service.ErrPasswordWeak):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest, ruleRefusedMessage)
		return
	case errors.Is(err, service.ErrUnauthenticated), errors.Is(err, service.ErrActorRequired):
		adminUnauthorized(c)
		return
	case err != nil:
		httpx.Internal(c)
		return
	}

	c.Status(http.StatusNoContent)
}

// ListAdmins returns every admin, active or not.
//
//	@Summary		List admins
//	@Description	Every admin of the panel, active or not.
//	@Tags			admin-admins
//	@Produce		json
//	@Success		200	{object}	httpx.Envelope{data=[]dto.AdminResponse}
//	@Failure		401	{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		403	{object}	httpx.ErrorResponse	"the session's role may not manage admins"
//	@Router			/admin/admins [get]
func (h *Handler) ListAdmins(c *gin.Context) {
	admins, err := h.admins.ListAdmins(c.Request.Context())
	if err != nil {
		httpx.Internal(c)
		return
	}
	httpx.Data(c, http.StatusOK, admins)
}

// CreateAdmin adds one panel actor. The actor is taken from the session, never from
// the body (ADR 0027 §3).
//
//	@Summary		Create an admin
//	@Description	Adds one panel actor and leaves exactly one audit row for it. Owner only.
//	@Tags			admin-admins
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.CreateAdminRequest	true	"email, role and password"
//	@Success		201		{object}	httpx.Envelope{data=dto.AdminResponse}
//	@Failure		401		{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		403		{object}	httpx.ErrorResponse	"the session's role may not manage admins"
//	@Failure		409		{object}	httpx.ErrorResponse	"the email is already in use"
//	@Failure		422		{object}	httpx.ErrorResponse	"invalid email, role or password"
//	@Router			/admin/admins [post]
func (h *Handler) CreateAdmin(c *gin.Context) {
	actor, ok := middleware.AdminActorFrom(c)
	if !ok {
		adminUnauthorized(c)
		return
	}

	var request dto.CreateAdminRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	}

	admin, err := h.admins.CreateAdmin(c.Request.Context(), actor, &request)
	switch {
	case errors.Is(err, service.ErrEmailTaken):
		httpx.Error(c, http.StatusConflict, httpx.CodeConflict, "Email sudah dipakai admin lain.")
		return
	case errors.Is(err, service.ErrRoleInvalid):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest,
			"Peran harus owner, support, atau auditor.")
		return
	case errors.Is(err, service.ErrEmailInvalid):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest, "Email tidak valid.")
		return
	case errors.Is(err, service.ErrPasswordWeak):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest, ruleRefusedMessage)
		return
	case err != nil:
		httpx.Internal(c)
		return
	}

	httpx.Data(c, http.StatusCreated, admin)
}

// ResetPassword sets a colleague's password and revokes every session of that
// admin in the same transaction as the audit row (ADR 0024 §5).
//
//	@Summary		Reset an admin's password
//	@Description	Rewrites the password, revokes that admin's sessions and records one audit row in the same transaction.
//	@Tags			admin-admins
//	@Accept			json
//	@Param			id		path	int							true	"admin id"
//	@Param			request	body	dto.ResetPasswordRequest	true	"new password"
//	@Success		204
//	@Failure		401	{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		403	{object}	httpx.ErrorResponse	"the session's role may not manage admins"
//	@Failure		404	{object}	httpx.ErrorResponse	"no such admin"
//	@Failure		422	{object}	httpx.ErrorResponse	"a password that breaks the rule"
//	@Router			/admin/admins/{id}/password [post]
func (h *Handler) ResetPassword(c *gin.Context) {
	actor, adminID, ok := h.actorAndID(c)
	if !ok {
		return
	}

	var request dto.ResetPasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	}

	err := h.admins.ResetPassword(c.Request.Context(), actor, adminID, &request)
	switch {
	case errors.Is(err, service.ErrAdminNotFound):
		httpx.Error(c, http.StatusNotFound, httpx.CodeNotFound, "Admin tidak ditemukan.")
		return
	case errors.Is(err, service.ErrPasswordWeak):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest, ruleRefusedMessage)
		return
	case err != nil:
		httpx.Internal(c)
		return
	}

	c.Status(http.StatusNoContent)
}

// SetActive enables or disables one admin. Deactivating revokes every session of
// that admin at once, so the old cookie stops working immediately (ADR 0024 §5).
//
//	@Summary		Enable or disable an admin
//	@Description	Deactivating revokes that admin's sessions and records one audit row in the same transaction.
//	@Tags			admin-admins
//	@Accept			json
//	@Produce		json
//	@Param			id		path	int						true	"admin id"
//	@Param			request	body	dto.SetActiveRequest	true	"the state to set"
//	@Success		200		{object}	httpx.Envelope{data=dto.SetActiveResponse}
//	@Failure		401		{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		403		{object}	httpx.ErrorResponse	"the session's role may not manage admins"
//	@Failure		404		{object}	httpx.ErrorResponse	"no such admin"
//	@Router			/admin/admins/{id}/active [post]
func (h *Handler) SetActive(c *gin.Context) {
	actor, adminID, ok := h.actorAndID(c)
	if !ok {
		return
	}

	var request dto.SetActiveRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	}

	err := h.admins.SetActive(c.Request.Context(), actor, adminID, &request)
	switch {
	case errors.Is(err, service.ErrAdminNotFound):
		httpx.Error(c, http.StatusNotFound, httpx.CodeNotFound, "Admin tidak ditemukan.")
		return
	case errors.Is(err, service.ErrRequestInvalid):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	case err != nil:
		httpx.Internal(c)
		return
	}

	httpx.Data(c, http.StatusOK, dto.SetActiveResponse{ID: adminID, Active: *request.Active})
}

// actorAndID reads the audited actor from the session and the target id from the
// path. A non-numeric id is a not-found, not a validation error: the panel never
// builds one, and answering 404 keeps the two id shapes indistinguishable.
func (h *Handler) actorAndID(c *gin.Context) (audit.Actor, uint, bool) {
	actor, ok := middleware.AdminActorFrom(c)
	if !ok {
		adminUnauthorized(c)
		return audit.Actor{}, 0, false
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		httpx.Error(c, http.StatusNotFound, httpx.CodeNotFound, "Admin tidak ditemukan.")
		return audit.Actor{}, 0, false
	}
	return actor, uint(id), true
}

// adminUnauthorized is the 401 the guard and these handlers share: an identity that
// is not in the context means the route was reached without Require, which the
// panel must still read as "sign in again", never as a 500.
func adminUnauthorized(c *gin.Context) {
	httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized,
		"Sesi admin tidak ditemukan atau sudah berakhir. Silakan masuk kembali.")
}

// ruleRefusedMessage is the one message for the panel's password rule, so the same
// refusal reads the same on every endpoint that can produce it.
//
//nolint:gosec // user-facing validation copy, not a credential
const ruleRefusedMessage = "Kata sandi minimal 12 karakter, maksimal 72 byte, dan tidak boleh sama dengan email."
