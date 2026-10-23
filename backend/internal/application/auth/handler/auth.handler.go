// Package handler adapts the passwordless entry path to HTTP: JSON in, session
// cookie out, stable error codes for every failure category. It holds no business
// logic (golden rule 2).
package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
)

// CookieSettings are the transport knobs for the session cookie, provided from
// deployment configuration.
type CookieSettings struct {
	Name   string
	Secure bool
	TTL    time.Duration
}

// Handler serves the auth routes.
type Handler struct {
	service service.Service
	cookies CookieSettings
}

// NewHandler builds the handler; Fx injects the service and the cookie settings.
func NewHandler(auth service.Service, cookies CookieSettings) *Handler {
	return &Handler{service: auth, cookies: cookies}
}

// RegisterRoutes mounts the entry path. None of these routes carries the
// Login Page CORS header: they are user-specific and same-origin by ADR 0009.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup) {
	auth := api.Group("/auth")
	{
		auth.POST("/magic-link", h.RequestMagicLink)
		auth.POST("/magic-link/exchange", h.ExchangeMagicLink)
		auth.GET("/session", h.GetSession)
		auth.POST("/logout", h.SignOut)
	}
}

// RequestMagicLink asks for a Magic Link by SMS. The success response is
// identical for every Phone Number, including one that has never transacted, so
// the endpoint cannot be used to enumerate customers.
//
//	@Summary		Request a Magic Link by SMS
//	@Description	Always answers 202 with the same body, whether or not the Phone Number is known.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.RequestMagicLinkRequest	true	"Phone Number, optional Package and return target"
//	@Success		202		{object}	httpx.Envelope{data=dto.MagicLinkSentResponse}
//	@Failure		400		{object}	httpx.ErrorResponse	"malformed body, invalid Phone Number or unknown Package"
//	@Failure		429		{object}	httpx.ErrorResponse	"too many requests from this address"
//	@Router			/auth/magic-link [post]
func (h *Handler) RequestMagicLink(c *gin.Context) {
	var request dto.RequestMagicLinkRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Permintaan tidak lengkap.")
		return
	}

	err := h.service.RequestMagicLink(c.Request.Context(), &request, c.ClientIP())
	switch {
	case errors.Is(err, phone.ErrInvalid):
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Phone Number tidak valid.")
		return
	case errors.Is(err, service.ErrPackageUnknown):
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Package tidak dikenal.")
		return
	case errors.Is(err, service.ErrRateLimited):
		httpx.Error(c, http.StatusTooManyRequests, httpx.CodeRateLimited,
			"Terlalu banyak permintaan dari alamat ini. Coba lagi beberapa saat lagi.")
		return
	case err != nil:
		httpx.Internal(c)
		return
	}

	// 202: the SMS is queued, not delivered. The body never varies.
	httpx.Data(c, http.StatusAccepted, dto.MagicLinkSentResponse{Sent: true})
}

// ExchangeMagicLink redeems the token from the SMS and opens a session.
//
//	@Summary		Redeem a Magic Link
//	@Description	Marks the token used, opens a session and sets the HttpOnly session cookie.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.ExchangeMagicLinkRequest	true	"the token from the SMS link"
//	@Success		200		{object}	httpx.Envelope{data=dto.ExchangeMagicLinkResponse}
//	@Failure		400		{object}	httpx.ErrorResponse	"the link is unknown, expired or already used"
//	@Router			/auth/magic-link/exchange [post]
func (h *Handler) ExchangeMagicLink(c *gin.Context) {
	var request dto.ExchangeMagicLinkRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeMagicLinkInvalid,
			"Tautan tidak berlaku. Minta tautan baru.")
		return
	}

	session, err := h.service.ExchangeMagicLink(c.Request.Context(), request.Token)
	switch {
	case errors.Is(err, service.ErrLinkInvalid):
		httpx.Error(c, http.StatusBadRequest, httpx.CodeMagicLinkInvalid,
			"Tautan tidak berlaku. Minta tautan baru.")
		return
	case err != nil:
		httpx.Internal(c)
		return
	}

	httpx.SetSessionCookie(c, h.cookies.Name, session.SessionToken,
		int(h.cookies.TTL.Seconds()), h.cookies.Secure)
	httpx.Data(c, http.StatusOK, session)
}

// GetSession reports the Phone Number behind the current session. The app calls
// it on load to decide between the checkout, the Account Status and the login
// screen.
//
//	@Summary		Current session
//	@Description	Returns the Phone Number and expiry of the session cookie, or 401 when there is none.
//	@Tags			auth
//	@Produce		json
//	@Success		200	{object}	httpx.Envelope{data=dto.SessionResponse}
//	@Failure		401	{object}	httpx.ErrorResponse	"no live session"
//	@Router			/auth/session [get]
func (h *Handler) GetSession(c *gin.Context) {
	token, ok := httpx.ReadSessionCookie(c, h.cookies.Name)
	if !ok {
		httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized,
			"Sesi tidak ditemukan. Minta tautan masuk baru.")
		return
	}

	session, err := h.service.Authenticate(c.Request.Context(), token)
	switch {
	case errors.Is(err, service.ErrUnauthenticated):
		// A stale cookie is cleared so the browser stops sending it.
		httpx.ClearSessionCookie(c, h.cookies.Name, h.cookies.Secure)
		httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized,
			"Sesi sudah berakhir. Minta tautan masuk baru.")
		return
	case err != nil:
		httpx.Internal(c)
		return
	}

	httpx.Data(c, http.StatusOK, session)
}

// SignOut revokes the session and clears the cookie. Revocation is what makes
// the cookie useless, not the deletion (ADR 0011).
//
//	@Summary		Sign out
//	@Description	Revokes the session row and clears the session cookie.
//	@Tags			auth
//	@Success		204
//	@Router			/auth/logout [post]
func (h *Handler) SignOut(c *gin.Context) {
	if token, ok := httpx.ReadSessionCookie(c, h.cookies.Name); ok {
		if err := h.service.SignOut(c.Request.Context(), token); err != nil {
			httpx.Internal(c)
			return
		}
	}

	httpx.ClearSessionCookie(c, h.cookies.Name, h.cookies.Secure)
	c.Status(http.StatusNoContent)
}
