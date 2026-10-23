// Package handler adapts the Transaction domain to HTTP. It holds no business
// logic: it binds the request, asks the service, and writes the shared envelope
// with a stable error code (golden rule 2).
package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// signatureHeader is where the vendor puts the HMAC of the raw callback body.
const signatureHeader = "X-Callback-Signature"

// Handler serves checkout and the public gateway callback.
type Handler struct {
	service service.Service
	guard   *middleware.SessionGuard
}

// NewHandler builds the handler; Fx injects the service and the session guard.
func NewHandler(transactionService service.Service, guard *middleware.SessionGuard) *Handler {
	return &Handler{service: transactionService, guard: guard}
}

// RegisterRoutes mounts the checkout endpoints behind the session guard and the
// gateway callback in front of it. Only the callback is public; it authenticates
// through its signature instead of a cookie.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup) {
	api.POST("/payments/callback", h.Callback)

	protected := api.Group("", h.guard.Require())
	{
		protected.GET("/transactions/channels", h.Channels)
		protected.POST("/transactions", h.Create)
		protected.GET("/transactions/:id", h.Get)
	}
}

// Channels lists the closed-payment methods the buyer may choose, with the fee
// that applies on top of the Package price.
//
//	@Summary		Payment channels for checkout
//	@Description	Lists the active closed-payment channels and their fees. Requires a session.
//	@Tags			transactions
//	@Produce		json
//	@Success		200	{object}	httpx.Envelope{data=[]dto.ChannelResponse}
//	@Failure		401	{object}	httpx.ErrorResponse	"no live session"
//	@Failure		502	{object}	httpx.ErrorResponse	"the payment gateway is unreachable"
//	@Router			/transactions/channels [get]
func (h *Handler) Channels(c *gin.Context) {
	channels, err := h.service.Channels(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}

	httpx.Data(c, http.StatusOK, channels)
}

// Create returns the pending Transaction for one Package, opening it when there
// is none. A repeated call is the same Transaction, so two "Bayar" taps cannot
// charge twice.
//
//	@Summary		Create or reuse the pending Transaction
//	@Description	Idempotent per session and Package while a pending Transaction is unexpired.
//	@Tags			transactions
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.CreateTransactionRequest	true	"Package and channel"
//	@Success		200		{object}	httpx.Envelope{data=dto.TransactionResponse}
//	@Failure		400		{object}	httpx.ErrorResponse	"unknown Package or channel"
//	@Failure		401		{object}	httpx.ErrorResponse	"no live session"
//	@Failure		404		{object}	httpx.ErrorResponse	"the Phone Number in the body is not the session's"
//	@Failure		502		{object}	httpx.ErrorResponse	"the payment gateway is unreachable"
//	@Router			/transactions [post]
func (h *Handler) Create(c *gin.Context) {
	var request dto.CreateTransactionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest,
			"Package dan kanal pembayaran wajib dipilih.")
		return
	}

	session, ok := middleware.SessionFrom(c)
	if !ok {
		httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Sesi tidak ditemukan.")
		return
	}

	// A Phone Number in the body is only a cross-check. Naming someone else's is
	// answered as not found, never as forbidden (PRD point 13).
	if !middleware.RequireOwnPhoneNumber(c, request.PhoneNumber) {
		return
	}

	transaction, err := h.service.Create(c.Request.Context(), session.PhoneNumber, &request)
	if err != nil {
		writeError(c, err)
		return
	}

	httpx.Data(c, http.StatusOK, transaction)
}

// Get returns one Transaction the session's Phone Number owns. It is what the
// app polls; settlement itself still only arrives by callback.
//
//	@Summary		Read one Transaction
//	@Description	Returns the current state and instructions. Another buyer's Transaction answers 404.
//	@Tags			transactions
//	@Produce		json
//	@Param			id	path		int	true	"Transaction id"
//	@Success		200	{object}	httpx.Envelope{data=dto.TransactionResponse}
//	@Failure		401	{object}	httpx.ErrorResponse	"no live session"
//	@Failure		404	{object}	httpx.ErrorResponse	"unknown Transaction, or one owned by another Phone Number"
//	@Router			/transactions/{id} [get]
func (h *Handler) Get(c *gin.Context) {
	session, ok := middleware.SessionFrom(c)
	if !ok {
		httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Sesi tidak ditemukan.")
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		httpx.Error(c, http.StatusNotFound, httpx.CodeNotFound, "Transaction tidak ditemukan.")
		return
	}

	transaction, err := h.service.Get(c.Request.Context(), session.PhoneNumber, uint(id))
	if err != nil {
		writeError(c, err)
		return
	}

	httpx.Data(c, http.StatusOK, transaction)
}

// Callback accepts a settlement notification. It is public by necessity — the
// gateway has no cookie — and authenticates the raw payload with the shared
// secret. It is the only path into `paid` (PRD point 16).
//
//	@Summary		Payment gateway callback
//	@Description	Verifies the signature over the raw body and applies the settlement transition.
//	@Tags			transactions
//	@Accept			json
//	@Produce		json
//	@Param			X-Callback-Signature	header		string	true	"HMAC-SHA256 of the raw body"
//	@Success		200						{object}	httpx.Envelope{data=dto.CallbackAck}
//	@Failure		401						{object}	httpx.ErrorResponse	"the signature does not match"
//	@Failure		409						{object}	httpx.ErrorResponse	"the notification amount does not match the Transaction"
//	@Router			/payments/callback [post]
func (h *Handler) Callback(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Callback tidak terbaca.")
		return
	}

	signature := strings.TrimSpace(c.GetHeader(signatureHeader))
	if err := h.service.HandleCallback(c.Request.Context(), raw, signature); err != nil {
		writeError(c, err)
		return
	}

	httpx.Data(c, http.StatusOK, dto.CallbackAck{Received: true})
}

// writeError maps the domain's sentinels onto HTTP. Unknown errors become a
// generic 500 so no internal detail reaches a buyer.
func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		httpx.Error(c, http.StatusNotFound, httpx.CodeNotFound, "Transaction tidak ditemukan.")
	case errors.Is(err, service.ErrPackageUnknown):
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest, "Package tidak dikenal.")
	case errors.Is(err, service.ErrChannelUnknown):
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest,
			"Kanal pembayaran tidak tersedia. Pilih kanal lain.")
	case errors.Is(err, payment.ErrSignatureMismatch):
		httpx.Error(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Callback tidak sah.")
	case errors.Is(err, service.ErrAmountMismatch):
		httpx.Error(c, http.StatusConflict, httpx.CodeConflict,
			"Jumlah pembayaran tidak sesuai dengan Transaction.")
	case errors.Is(err, service.ErrGatewayUnavailable):
		httpx.Error(c, http.StatusBadGateway, httpx.CodeDependencyFailed,
			"Pembayaran sedang tidak dapat diproses. Coba lagi beberapa saat lagi.")
	default:
		httpx.Internal(c)
	}
}
