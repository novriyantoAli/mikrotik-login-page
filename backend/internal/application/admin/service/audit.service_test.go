package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// auditService builds the service with only the reader's collaborators set up: the
// bootstrap settings and the clock are irrelevant to a read.
func auditService(repo *fakeRepository) service.Service {
	return service.NewService(repo, service.BootstrapSettings{}, testClock, zap.NewNop())
}

// TestListAuditMapsRowsAndPagination is the reader's happy path: the page the
// repository returned is mapped into the panel's envelope without inventing a
// field, and the payload travels exactly as it was written.
func TestListAuditMapsRowsAndPagination(t *testing.T) {
	created := time.Date(2026, time.November, 3, 8, 5, 0, 0, time.UTC)
	repo := &fakeRepository{
		auditRows: []entity.AuditEntry{{
			ID:         42,
			AdminID:    1,
			AdminEmail: "owner@example.test",
			Action:     audit.ActionPackageUpdate,
			TargetType: string(audit.TargetPackage),
			TargetID:   "1",
			Payload: entity.Payload{
				"price_idr": map[string]any{"before": float64(5000), "after": float64(7500)},
			},
			IP:        "203.0.113.9",
			CreatedAt: created,
		}},
		auditTotal: 1,
	}

	page, err := auditService(repo).ListAudit(context.Background(), &dto.AdminAuditFilter{})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}

	if page.TotalCount != 1 || page.Page != 1 || page.PageSize != 20 {
		t.Fatalf("page envelope = %+v, want the defaults", page)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}

	row := page.Items[0]
	if row.ID != 42 || row.AdminEmail != "owner@example.test" {
		t.Fatalf("row identity = %+v", row)
	}
	if row.Action != string(audit.ActionPackageUpdate) {
		t.Fatalf("action = %q, want %q", row.Action, audit.ActionPackageUpdate)
	}
	if row.TargetType != string(audit.TargetPackage) || row.TargetID != "1" {
		t.Fatalf("target = %q/%q, want package/1", row.TargetType, row.TargetID)
	}
	if row.CreatedAt != created.Format(time.RFC3339) {
		t.Fatalf("created_at = %q, want %q", row.CreatedAt, created.Format(time.RFC3339))
	}

	// The payload is passed through, before/after included: the reader renders what
	// the writer recorded rather than a second, derived shape.
	change, ok := row.Payload["price_idr"].(map[string]any)
	if !ok {
		t.Fatalf("payload price_idr = %#v, want a before/after object", row.Payload["price_idr"])
	}
	if change["before"] != float64(5000) || change["after"] != float64(7500) {
		t.Fatalf("price change = %#v", change)
	}
}

// TestListAuditDefaultsAnAbsentPayload keeps the response shape stable: JSON needs
// an object, never null, or the panel's strict schema would refuse the row.
func TestListAuditDefaultsAnAbsentPayload(t *testing.T) {
	repo := &fakeRepository{
		auditRows: []entity.AuditEntry{{
			ID:         1,
			AdminEmail: "owner@example.test",
			Action:     audit.ActionAdminCreate,
			Payload:    nil,
			CreatedAt:  testClock.now,
		}},
		auditTotal: 1,
	}

	page, err := auditService(repo).ListAudit(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if page.Items[0].Payload == nil {
		t.Fatal("a row without a payload must answer an empty object, not null")
	}
	if len(page.Items[0].Payload) != 0 {
		t.Fatalf("payload = %#v, want empty", page.Items[0].Payload)
	}
}

// TestListAuditRefusesAnUnknownAction is the difference between "a typo" and "the
// trail is empty": the reader refuses the filter instead of answering no rows.
func TestListAuditRefusesAnUnknownAction(t *testing.T) {
	repo := &fakeRepository{}

	_, err := auditService(repo).ListAudit(context.Background(), &dto.AdminAuditFilter{
		Action: "package.explode",
	})
	if !errors.Is(err, service.ErrAuditFilterInvalid) {
		t.Fatalf("error = %v, want ErrAuditFilterInvalid", err)
	}
	var filterError *service.AuditFilterError
	if !errors.As(err, &filterError) || filterError.Field != "action" {
		t.Fatalf("error = %v, want an action filter reason", err)
	}
	if len(repo.auditParams) != 0 {
		t.Fatal("a refused filter must not reach the repository")
	}
}

// TestListAuditAcceptsEveryKnownAction is the other side of the same rule, and the
// guard against a hand-copied list drifting from the writer's vocabulary.
func TestListAuditAcceptsEveryKnownAction(t *testing.T) {
	for _, action := range audit.Actions() {
		repo := &fakeRepository{}
		if _, err := auditService(repo).ListAudit(context.Background(), &dto.AdminAuditFilter{
			Action: string(action),
		}); err != nil {
			t.Fatalf("action %q is refused: %v", action, err)
		}
		if len(repo.auditParams) != 1 || repo.auditParams[0].Action == nil {
			t.Fatalf("action %q did not reach the repository", action)
		}
		if *repo.auditParams[0].Action != action {
			t.Fatalf("action = %q, want %q", *repo.auditParams[0].Action, action)
		}
	}
}

// TestListAuditParsesDaysInTheDeploymentTimeZone pins the half-open window: a day
// is the WIB day the auditor read, and `to` stops at the next day's first instant.
func TestListAuditParsesDaysInTheDeploymentTimeZone(t *testing.T) {
	repo := &fakeRepository{}

	_, err := auditService(repo).ListAudit(context.Background(), &dto.AdminAuditFilter{
		From: "2026-10-23",
		To:   "2026-10-24",
	})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}

	wib := time.FixedZone("WIB", 7*60*60)
	params := repo.auditParams[0]
	if params.From == nil || !params.From.Equal(time.Date(2026, time.October, 23, 0, 0, 0, 0, wib)) {
		t.Fatalf("from = %v, want the first instant of 23 October WIB", params.From)
	}
	if params.To == nil || !params.To.Equal(time.Date(2026, time.October, 25, 0, 0, 0, 0, wib)) {
		t.Fatalf("to = %v, want the first instant after 24 October WIB", params.To)
	}
}

// TestListAuditRefusesMalformedDays keeps a typo from becoming an empty page.
func TestListAuditRefusesMalformedDays(t *testing.T) {
	for _, filter := range []*dto.AdminAuditFilter{
		{From: "23-10-2026"},
		{To: "2026-10-24T08:00:00Z"},
		{From: "2026-10-25", To: "2026-10-24"},
	} {
		_, err := auditService(&fakeRepository{}).ListAudit(context.Background(), filter)
		if !errors.Is(err, service.ErrAuditFilterInvalid) {
			t.Fatalf("filter %+v: error = %v, want ErrAuditFilterInvalid", filter, err)
		}
	}
}

// TestListAuditBoundsThePage keeps a zero or oversized page_size from turning into
// "no LIMIT" or an unbounded read.
func TestListAuditBoundsThePage(t *testing.T) {
	repo := &fakeRepository{}

	if _, err := auditService(repo).ListAudit(context.Background(), &dto.AdminAuditFilter{
		Page:     3,
		PageSize: 500,
	}); err != nil {
		t.Fatalf("ListAudit: %v", err)
	}

	params := repo.auditParams[0]
	if params.Page != 3 || params.PageSize != 100 {
		t.Fatalf("page = %d/%d, want 3/100", params.Page, params.PageSize)
	}
}

// TestListAuditPassesTheRepositoryFailureThrough keeps the transport's generic 500
// honest: the service does not invent a page when the read fails.
func TestListAuditPassesTheRepositoryFailureThrough(t *testing.T) {
	failure := errors.New("postgres is down")
	repo := &fakeRepository{auditErr: failure}

	if _, err := auditService(repo).ListAudit(context.Background(), &dto.AdminAuditFilter{}); !errors.Is(err, failure) {
		t.Fatalf("error = %v, want the repository failure", err)
	}
}
