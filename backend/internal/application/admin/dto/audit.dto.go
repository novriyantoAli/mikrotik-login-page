package dto

// AdminAuditFilter is the audit reader's search: the filters an owner or an
// auditor types while reviewing what the panel did. Dates are `YYYY-MM-DD` days in
// the deployment's own day (WIB), with both bounds included.
//
// Action is not gated by a binding tag: the shared vocabulary in pkg/audit owns the
// names, so there is one list of actions rather than one per transport, and a typo
// answers the same 422 as any other refused filter.
type AdminAuditFilter struct {
	AdminEmail string `form:"admin_email"`
	Action     string `form:"action"`
	From       string `form:"from"`
	To         string `form:"to"`
	Page       int    `form:"page" binding:"omitempty,min=1"`
	PageSize   int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// AdminAuditEntryResponse is one row of the append-only trail. AdminEmail is copied
// into the row at write time (ADR 0027 §3), so a row still reads after the admin it
// names is deactivated; TargetID is text because one column holds a Package id and
// a Transaction id alike.
//
// Payload is the small free-form document the writer left — before/after for a
// catalog edit, the outcome of a PIN reveal — and it never holds a credential
// (ADR 0027 §4). The reader renders it generically rather than modeling one shape
// per action.
type AdminAuditEntryResponse struct {
	ID         uint           `json:"id" example:"42"`
	CreatedAt  string         `json:"created_at" example:"2026-10-23T08:05:00Z"`
	AdminEmail string         `json:"admin_email" example:"owner@example.test"`
	Action     string         `json:"action" example:"package.update"`
	TargetType string         `json:"target_type" example:"package"`
	TargetID   string         `json:"target_id" example:"1"`
	Payload    map[string]any `json:"payload"`
	IP         string         `json:"ip" example:"203.0.113.9"`
}

// AdminAuditPage is one page of the trail in the shared paginated envelope.
type AdminAuditPage struct {
	Items      []AdminAuditEntryResponse
	TotalCount int64
	Page       int
	PageSize   int
}
