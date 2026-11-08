// Audit reader is the other half of the trail: the adapter next door writes one
// row per privileged action, and this statement reads them back for the owner and
// the auditor (ADR 0027). It is deliberately read-only — there is no update and no
// delete path, here or anywhere else (ADR 0027 §3).
package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// Audit page bounds, the same numbers the panel's other lists use so an operator
// has one pagination shape to learn. They are exported because the service applies
// the same policy when it reports the page it answered; one declaration keeps the
// reported numbers and the enforced ones from drifting.
const (
	DefaultAuditPageSize = 20
	MaxAuditPageSize     = 100
)

// AuditListParams is the audit reader's query, owned here rather than reusing
// dto.AdminAuditFilter (golden rule 14).
//
// AdminEmail is copied onto every row at write time, so filtering reads that column
// instead of joining `admins`: a row still reads after its actor is deactivated or
// gone (ADR 0027 §3). The time window is half-open — From inclusive, To exclusive —
// so two adjacent days never both match the same row.
type AuditListParams struct {
	AdminEmail string
	Action     *audit.Action
	From       *time.Time
	To         *time.Time
	Page       int
	PageSize   int
}

// ListAudit reads one page of the trail, newest first. The count and the page are
// built by the same expression, so the total a caller paginates with always
// describes the rows it asked for.
func (r *adminRepository) ListAudit(
	ctx context.Context,
	params AuditListParams,
) ([]entity.AuditEntry, int64, error) {
	params.Page, params.PageSize = auditPageBounds(params.Page, params.PageSize)

	// A fresh statement per query: reusing one *gorm.DB for count and page would let
	// the second call inherit the first one's SELECT list.
	query := func() *gorm.DB {
		statement := r.db.WithContext(ctx).Model(&entity.AuditEntry{})
		if email := strings.TrimSpace(params.AdminEmail); email != "" {
			// An operator types an address in any case; the row stores the canonical
			// form it was written with.
			statement = statement.Where("lower(admin_email) = ?", strings.ToLower(email))
		}
		if params.Action != nil {
			statement = statement.Where("action = ?", *params.Action)
		}
		if params.From != nil {
			statement = statement.Where("created_at >= ?", *params.From)
		}
		if params.To != nil {
			statement = statement.Where("created_at < ?", *params.To)
		}
		return statement
	}

	var total int64
	if err := query().Count(&total).Error; err != nil {
		r.logger.Error("count audit rows", zap.Error(err))
		return nil, 0, fmt.Errorf("count audit rows: %w", err)
	}

	rows := make([]entity.AuditEntry, 0, params.PageSize)
	// id is the tie-break: two rows of one transaction share created_at, and without
	// it a later page could repeat one row and skip another.
	err := query().
		Order("created_at DESC, id DESC").
		Limit(params.PageSize).
		Offset((params.Page - 1) * params.PageSize).
		Find(&rows).Error
	if err != nil {
		r.logger.Error("read audit rows", zap.Error(err))
		return nil, 0, fmt.Errorf("read audit rows: %w", err)
	}
	return rows, total, nil
}

// auditPageBounds keeps the page numbers inside the range the reader promises. The
// bounds belong with the SQL, not with one caller's politeness: a zero PageSize
// would otherwise mean "no LIMIT" and hand back the whole log.
func auditPageBounds(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultAuditPageSize
	}
	if pageSize > MaxAuditPageSize {
		pageSize = MaxAuditPageSize
	}
	return page, pageSize
}
