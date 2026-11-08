// Package handler adapts the panel's Transaction routes to HTTP. The rules live in
// the transaction domain; this file resolves the admin, enforces the role matrix and
// maps the service's refusals onto the shared envelope (golden rule 2).
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	transactionsdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// TransactionHandler serves `/api/admin/transactions`. It depends on the transaction
// domain's Service interface, never on its repository: cross-domain calls go through
// the owning domain's public surface (golden rule 5).
type TransactionHandler struct {
	transactions transactionservice.Service
	guard        *middleware.AdminGuard
	// revealLimit is the panel's own budget for opening a PIN: the transaction
	// domain owns the act, the transport owns the throttle (ADR 0027 §4).
	revealLimit *middleware.PINRevealLimiter
}

// NewTransactionHandler builds the handler; Fx injects the transaction service, the
// admin guard and the reveal budget.
func NewTransactionHandler(
	transactions transactionservice.Service,
	guard *middleware.AdminGuard,
	revealLimit *middleware.PINRevealLimiter,
) *TransactionHandler {
	return &TransactionHandler{transactions: transactions, guard: guard, revealLimit: revealLimit}
}

// RegisterRoutes mounts the panel's Transaction search. Every role may read it
// (ADR 0024 §3): answering "where is my payment?" is the panel's first job.
func (h *TransactionHandler) RegisterRoutes(api *gin.RouterGroup) {
	admin := api.Group("/admin")

	panel := admin.Group("/transactions", h.guard.Require())
	{
		panel.GET("", h.ListTransactions)
	}

	// Opening a PIN is the one act the matrix splits: support may do it, an auditor
	// may not, and the budget is spent per admin rather than per screenshot
	// (ADR 0024 §3, ADR 0027 §4).
	revealers := admin.Group("/transactions", h.guard.Require(entity.RoleOwner, entity.RoleSupport))
	{
		revealers.POST("/:id/pin", h.revealLimit.Limit(), h.RevealPIN)
	}
}

// ListTransactions returns one page of Transactions for the panel's search. The
// response never carries a PIN: opening one is a separate, audited act (PRD #35).
//
//	@Summary		Search Transactions
//	@Description	Filter by state, Phone Number, Package and date range. Newest first, without the PIN.
//	@Tags			admin-transactions
//	@Produce		json
//	@Param			state			query		string	false	"pending, paid, provisioned, failed or expired"
//	@Param			phone_number	query		string	false	"Exact Phone Number; the form a buyer types is accepted"
//	@Param			package_id		query		int		false	"Package id as bought"
//	@Param			from			query		string	false	"First day included, YYYY-MM-DD"
//	@Param			to				query		string	false	"Last day included, YYYY-MM-DD"
//	@Param			page			query		int		false	"Page, starting at 1"
//	@Param			page_size		query		int		false	"Rows per page, at most 100"
//	@Success		200	{object}	httpx.PagedEnvelope{data=[]transactionsdto.AdminTransactionSummaryResponse}
//	@Failure		400	{object}	httpx.ErrorResponse	"page, page_size or package_id is not a number in range"
//	@Failure		401	{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		422	{object}	httpx.ErrorResponse	"a filter value the search cannot answer"
//	@Router			/admin/transactions [get]
func (h *TransactionHandler) ListTransactions(c *gin.Context) {
	var filter transactionsdto.AdminTransactionFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		// Only the numeric fields are the transport's to reject here. Every other
		// value is judged by the search itself, so one mistake has one status.
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest,
			"Filter tidak valid: page dan page_size harus angka dalam rentangnya.")
		return
	}

	page, err := h.transactions.ListTransactions(c.Request.Context(), &filter)
	if err != nil {
		writeTransactionError(c, err)
		return
	}

	httpx.Paged(c, http.StatusOK, page.Items, page.TotalCount, page.Page, page.PageSize)
}

// RevealPIN opens one Transaction's PIN for an operator who is answering a buyer
// whose SMS never arrived. Every attempt leaves an audit row, whether or not it
// produced a PIN, and the value never appears in a list, a URL or a log.
//
//	@Summary		Open one Transaction's PIN
//	@Description	Decrypts the sealed PIN of a provisioned Transaction and always appends one
//	@Description	audit row (pin.reveal). Owner and support only, rate-limited per admin.
//	@Tags			admin-transactions
//	@Produce		json
//	@Param			id	path		int	true	"Transaction id"
//	@Success		200	{object}	httpx.Envelope{data=transactionsdto.AdminPINRevealResponse}
//	@Failure		400	{object}	httpx.ErrorResponse	"the id is not a number"
//	@Failure		401	{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		403	{object}	httpx.ErrorResponse	"the session's role may not open a PIN"
//	@Failure		404	{object}	httpx.ErrorResponse	"no such Transaction"
//	@Failure		409	{object}	httpx.ErrorResponse	"the Transaction has no PIN yet"
//	@Failure		429	{object}	httpx.ErrorResponse	"this admin has spent its reveal budget"
//	@Failure		500	{object}	httpx.ErrorResponse	"the PIN could not be opened"
//	@Router			/admin/transactions/{id}/pin [post]
func (h *TransactionHandler) RevealPIN(c *gin.Context) {
	actor, ok := middleware.AdminActorFrom(c)
	if !ok {
		adminUnauthorized(c)
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest,
			"ID transaksi tidak valid.")
		return
	}

	value, err := h.transactions.RevealPIN(c.Request.Context(), actor, uint(id))
	if err != nil {
		writePINRevealError(c, err)
		return
	}

	httpx.Data(c, http.StatusOK, transactionsdto.AdminPINRevealResponse{PIN: value})
}

// writePINRevealError maps a refusal onto the panel's statuses. A key fault stays a
// generic 500: the operator needs to know "not now", not which key version is
// missing (ADR 0016).
func writePINRevealError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, transactionservice.ErrNotFound):
		httpx.Error(c, http.StatusNotFound, httpx.CodeNotFound, "Transaksi tidak ditemukan.")
	case errors.Is(err, transactionservice.ErrPINNotReady):
		httpx.Error(c, http.StatusConflict, httpx.CodeConflict,
			"PIN belum tersedia: transaksi ini belum selesai provisioning.")
	case errors.Is(err, transactionservice.ErrPINUnavailable):
		httpx.Internal(c)
	case errors.Is(err, transactionservice.ErrActorRequired):
		adminUnauthorized(c)
	default:
		httpx.Internal(c)
	}
}

// writeTransactionError maps the search's refusals onto the panel's statuses. The
// filter reasons are operator-facing copy; an unexpected failure stays a generic 500
// so no internal text leaks.
func writeTransactionError(c *gin.Context, err error) {
	var filterError *transactionservice.FilterError

	switch {
	case errors.As(err, &filterError):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest, filterError.Reason)
	case errors.Is(err, transactionservice.ErrFilterInvalid):
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest,
			"Filter transaksi tidak valid.")
	default:
		httpx.Internal(c)
	}
}
