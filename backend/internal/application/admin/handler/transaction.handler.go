// Package handler adapts the panel's Transaction routes to HTTP. The rules live in
// the transaction domain; this file resolves the admin, enforces the role matrix and
// maps the service's refusals onto the shared envelope (golden rule 2).
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

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
}

// NewTransactionHandler builds the handler; Fx injects the transaction service and
// the admin guard.
func NewTransactionHandler(
	transactions transactionservice.Service,
	guard *middleware.AdminGuard,
) *TransactionHandler {
	return &TransactionHandler{transactions: transactions, guard: guard}
}

// RegisterRoutes mounts the panel's Transaction search. Every role may read it
// (ADR 0024 §3): answering "where is my payment?" is the panel's first job.
func (h *TransactionHandler) RegisterRoutes(api *gin.RouterGroup) {
	admin := api.Group("/admin")

	panel := admin.Group("/transactions", h.guard.Require())
	{
		panel.GET("", h.ListTransactions)
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
