// This file holds the audit reader's rules: which filters a search may name, how a
// day becomes a time window, and how a row becomes the panel's response. The trail
// is append-only, so there is nothing here that changes or removes a row
// (ADR 0027 §3).
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/panelwindow"
)

// ErrAuditFilterInvalid is the reader's "this search cannot be answered" sentinel.
// The handler maps it to one 422 with the reason, and a caller branches on the value
// rather than on message text.
var ErrAuditFilterInvalid = errors.New("audit filter is invalid")

// AuditFilterError names the refused filter and why, in the words the panel shows.
// It implements Is so every rule reads as ErrAuditFilterInvalid while the message
// still tells the auditor what to fix.
type AuditFilterError struct {
	Field  string
	Reason string
}

// Error is the operator-facing reason, never a Go value dump.
func (e *AuditFilterError) Error() string { return e.Reason }

// Is makes errors.Is(err, ErrAuditFilterInvalid) true for every filter rule.
func (e *AuditFilterError) Is(target error) bool { return target == ErrAuditFilterInvalid }

// ListAudit answers the panel's audit search. It is a read: nothing here writes a
// row, and there is no counterpart that could rewrite one (ADR 0027 §3).
func (s *service) ListAudit(
	ctx context.Context,
	filter *dto.AdminAuditFilter,
) (*dto.AdminAuditPage, error) {
	params, err := auditParams(filter)
	if err != nil {
		return nil, err
	}

	rows, total, err := s.repo.ListAudit(ctx, params)
	if err != nil {
		// The repository already logged the cause; passing it up is the service's job,
		// not a second log line (golden rule 13).
		return nil, err
	}

	items := make([]dto.AdminAuditEntryResponse, 0, len(rows))
	for index := range rows {
		items = append(items, toAuditResponse(&rows[index]))
	}
	return &dto.AdminAuditPage{
		Items:      items,
		TotalCount: total,
		Page:       params.Page,
		PageSize:   params.PageSize,
	}, nil
}

// auditParams turns the panel's query into the repository's own params. Every rule
// about a searchable value lives here, not in the transport's binding tags.
func auditParams(filter *dto.AdminAuditFilter) (repository.AuditListParams, error) {
	params := repository.AuditListParams{
		Page:     1,
		PageSize: repository.DefaultAuditPageSize,
	}
	if filter == nil {
		return params, nil
	}

	if filter.Page > 0 {
		params.Page = filter.Page
	}
	if filter.PageSize > 0 {
		params.PageSize = filter.PageSize
	}
	if params.PageSize > repository.MaxAuditPageSize {
		params.PageSize = repository.MaxAuditPageSize
	}
	params.AdminEmail = strings.TrimSpace(filter.AdminEmail)

	action, err := auditAction(filter.Action)
	if err != nil {
		return params, err
	}
	params.Action = action

	window, err := panelwindow.Of(filter.From, filter.To)
	if err != nil {
		return params, auditDayError(err)
	}
	params.From = window.From
	params.To = window.To
	return params, nil
}

// auditDayError maps the shared window's refusal onto the reader's own filter error,
// so a caller still branches on one sentinel (ErrAuditFilterInvalid).
func auditDayError(err error) error {
	var dateError *panelwindow.DateError
	if errors.As(err, &dateError) {
		return &AuditFilterError{Field: dateError.Field, Reason: dateError.Reason}
	}
	return err
}

// auditAction keeps one action only when the vocabulary knows it. An unknown value
// is a typo, and answering it with an empty page would read as "the trail is empty".
func auditAction(raw string) (*audit.Action, error) {
	if raw == "" {
		return nil, nil
	}

	action := audit.Action(raw)
	if !audit.Known(action) {
		return nil, &AuditFilterError{Field: "action", Reason: "Aksi audit tidak dikenal."}
	}
	return &action, nil
}

// toAuditResponse maps one row for the panel. The payload is passed through as the
// document it is: the reader renders before/after generically, and no action's
// shape is invented here.
func toAuditResponse(row *entity.AuditEntry) dto.AdminAuditEntryResponse {
	payload := map[string]any(row.Payload)
	if payload == nil {
		payload = map[string]any{}
	}

	return dto.AdminAuditEntryResponse{
		ID:         row.ID,
		CreatedAt:  row.CreatedAt.UTC().Format(time.RFC3339),
		AdminEmail: row.AdminEmail,
		Action:     string(row.Action),
		TargetType: row.TargetType,
		TargetID:   row.TargetID,
		Payload:    payload,
		IP:         row.IP,
	}
}
