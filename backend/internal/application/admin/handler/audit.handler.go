// Audit handler adapts the panel's audit reader to HTTP. It holds no rules: the
// guard enforces the role matrix, the service owns the filters, and the shared
// envelope carries the page (golden rule 2).
//
// There is deliberately no write route in this file. The trail is append-only, so
// the reader's surface is one GET and nothing else (ADR 0027 §3).
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// AuditHandler serves `/api/admin/audit`.
type AuditHandler struct {
	panel service.Service
	guard *middleware.AdminGuard
}

// NewAuditHandler builds the handler; Fx injects the admin service and the guard.
func NewAuditHandler(panel service.Service, guard *middleware.AdminGuard) *AuditHandler {
	return &AuditHandler{panel: panel, guard: guard}
}

// RegisterRoutes mounts the audit reader. The matrix grants it to the owner and to
// the auditor, whose whole job is reading it; support is refused (ADR 0024 §3).
func (h *AuditHandler) RegisterRoutes(api *gin.RouterGroup) {
	admin := api.Group("/admin")
	readers := admin.Group("/audit", h.guard.Require(entity.RoleOwner, entity.RoleAuditor))
	{
		readers.GET("", h.ListAudit)
	}
}

// ListAudit returns one page of the append-only trail, newest first. It is the
// answer to "who raised this price?" without shell access.
//
//	@Summary		Read the audit trail
//	@Description	Filter by admin email, action and date range. Owner and auditor only.
//	@Description	Rows are append-only: there is no endpoint that changes or removes one.
//	@Tags			admin-audit
//	@Produce		json
//	@Param			admin_email	query		string	false	"Actor's email address, matched case-insensitively"
//	@Param			action		query		string	false	"One action name, e.g. package.update or pin.reveal"
//	@Param			from		query		string	false	"First day included, YYYY-MM-DD (WIB)"
//	@Param			to			query		string	false	"Last day included, YYYY-MM-DD (WIB)"
//	@Param			page		query		int		false	"Page, starting at 1"
//	@Param			page_size	query		int		false	"Rows per page, at most 100"
//	@Success		200	{object}	httpx.PagedEnvelope{data=[]dto.AdminAuditEntryResponse}
//	@Failure		400	{object}	httpx.ErrorResponse	"page or page_size is not a number in range"
//	@Failure		401	{object}	httpx.ErrorResponse	"no live admin session"
//	@Failure		403	{object}	httpx.ErrorResponse	"the session's role may not read the audit trail"
//	@Failure		422	{object}	httpx.ErrorResponse	"an unknown action or a malformed day"
//	@Router			/admin/audit [get]
func (h *AuditHandler) ListAudit(c *gin.Context) {
	var filter dto.AdminAuditFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		// Only the numeric fields are the transport's to reject here. Every other
		// value is judged by the reader itself, so one mistake has one status.
		httpx.Error(c, http.StatusBadRequest, httpx.CodeInvalidRequest,
			"Filter tidak valid: page dan page_size harus angka dalam rentangnya.")
		return
	}

	page, err := h.panel.ListAudit(c.Request.Context(), &filter)
	if err != nil {
		writeAuditError(c, err)
		return
	}

	httpx.Paged(c, http.StatusOK, page.Items, page.TotalCount, page.Page, page.PageSize)
}

// writeAuditError maps the reader's refusals onto the panel's statuses. Every refused
// filter reaches here as an *AuditFilterError, whose Reason is auditor-facing copy; an
// unexpected failure stays a generic 500 so no internal text leaks.
func writeAuditError(c *gin.Context, err error) {
	var filterError *service.AuditFilterError
	if errors.As(err, &filterError) {
		httpx.Error(c, http.StatusUnprocessableEntity, httpx.CodeInvalidRequest, filterError.Reason)
		return
	}
	httpx.Internal(c)
}
