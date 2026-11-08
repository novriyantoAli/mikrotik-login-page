package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	admindto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	adminhandler "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/handler"
	adminservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
)

// stubPanel answers the admin service's audit read; the management methods exist
// only because service.Service is one interface.
type stubPanel struct {
	page       *admindto.AdminAuditPage
	listErr    error
	lastFilter *admindto.AdminAuditFilter
}

func (s *stubPanel) ListAudit(
	_ context.Context,
	filter *admindto.AdminAuditFilter,
) (*admindto.AdminAuditPage, error) {
	s.lastFilter = filter
	return s.page, s.listErr
}

func (s *stubPanel) BootstrapOwner(context.Context) (int64, error) { return 0, nil }

func (s *stubPanel) CreateAdmin(
	context.Context,
	audit.Actor,
	*admindto.CreateAdminRequest,
) (*admindto.AdminResponse, error) {
	return nil, nil
}

func (s *stubPanel) ListAdmins(context.Context) ([]admindto.AdminResponse, error) {
	return nil, nil
}

func (s *stubPanel) ResetPassword(context.Context, audit.Actor, uint, *admindto.ResetPasswordRequest) error {
	return nil
}

func (s *stubPanel) SetActive(context.Context, audit.Actor, uint, *admindto.SetActiveRequest) error {
	return nil
}

// auditEngine mounts the reader the way the API does: one guard, one route.
func auditEngine(t *testing.T, panel *stubPanel, resolver stubResolver) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	guard := middleware.NewAdminGuard(resolver,
		middleware.AdminCookieSettings{Name: "hotspot_admin"}, zap.NewNop())

	engine := gin.New()
	adminhandler.NewAuditHandler(panel, guard).RegisterRoutes(engine.Group("/api"))
	return engine
}

// panelAuditPage is one page of the trail with the two payload shapes the panel
// renders.
func panelAuditPage() *admindto.AdminAuditPage {
	return &admindto.AdminAuditPage{
		Items: []admindto.AdminAuditEntryResponse{
			{
				ID: 42, CreatedAt: "2026-10-23T08:05:00Z", AdminEmail: "owner@example.test",
				Action: string(audit.ActionPackageUpdate), TargetType: string(audit.TargetPackage),
				TargetID: "1", IP: "203.0.113.9",
				Payload: map[string]any{"price_idr": map[string]any{"before": 5000, "after": 7500}},
			},
			{
				ID: 41, CreatedAt: "2026-10-23T08:04:00Z", AdminEmail: "support@example.test",
				Action: string(audit.ActionPINReveal), TargetType: string(audit.TargetTransaction),
				TargetID: "12", IP: "203.0.113.10",
				Payload: map[string]any{"result": "ok", "reference": "TRX-ONE"},
			},
		},
		TotalCount: 2,
		Page:       1,
		PageSize:   20,
	}
}

// TestAuditHandlerServesOwnerAndAuditor is the HTTP view of the matrix cell the
// integration suite also proves: the two reading roles get the page.
func TestAuditHandlerServesOwnerAndAuditor(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"owner", "auditor"} {
		engine := auditEngine(t, &stubPanel{page: panelAuditPage()}, stubResolver{role: role})

		recorder := doPackageRequest(t, engine, http.MethodGet, "/api/admin/audit", "", true)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s read = %d (%s)", role, recorder.Code, recorder.Body.String())
		}

		var body struct {
			Data       []map[string]any `json:"data"`
			TotalCount int64            `json:"total_count"`
			Page       int              `json:"page"`
			PageSize   int              `json:"page_size"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s read: %v", role, err)
		}
		if len(body.Data) != 2 || body.TotalCount != 2 || body.Page != 1 || body.PageSize != 20 {
			t.Fatalf("%s envelope = %+v", role, body)
		}
		if _, leaked := body.Data[1]["pin"]; leaked {
			t.Fatalf("%s response carries a pin field: %s", role, recorder.Body.String())
		}
	}
}

// TestAuditHandlerRefusesSupportAndAnonymity keeps 401 and 403 apart: no session
// is not the same answer as a role that may not read.
func TestAuditHandlerRefusesSupportAndAnonymity(t *testing.T) {
	t.Parallel()

	engine := auditEngine(t, &stubPanel{page: panelAuditPage()}, stubResolver{role: "support"})
	recorder := doPackageRequest(t, engine, http.MethodGet, "/api/admin/audit", "", true)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("support read = %d, want 403 (%s)", recorder.Code, recorder.Body.String())
	}
	if code := errorCodeOf(t, recorder.Body.Bytes()); code != httpx.CodeForbidden {
		t.Fatalf("error code = %q, want %q", code, httpx.CodeForbidden)
	}

	anonymous := doPackageRequest(t, engine, http.MethodGet, "/api/admin/audit", "", false)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous read = %d, want 401", anonymous.Code)
	}
}

// TestAuditHandlerPassesTheFiltersThrough proves the transport binds every filter
// the reader knows; the service is what judges them.
func TestAuditHandlerPassesTheFiltersThrough(t *testing.T) {
	t.Parallel()

	panel := &stubPanel{page: panelAuditPage()}
	engine := auditEngine(t, panel, stubResolver{role: "owner"})

	recorder := doPackageRequest(t, engine, http.MethodGet,
		"/api/admin/audit?admin_email=owner@example.test&action=package.update"+
			"&from=2026-10-23&to=2026-10-24&page=2&page_size=50", "", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read = %d (%s)", recorder.Code, recorder.Body.String())
	}

	want := admindto.AdminAuditFilter{
		AdminEmail: "owner@example.test", Action: "package.update",
		From: "2026-10-23", To: "2026-10-24", Page: 2, PageSize: 50,
	}
	if panel.lastFilter == nil || *panel.lastFilter != want {
		t.Fatalf("filter = %+v, want %+v", panel.lastFilter, want)
	}
}

// TestAuditHandlerMapsRefusedFiltersOntoStatuses keeps one mistake to one status:
// a malformed number is the transport's 400, a judgeable value is the reader's 422.
func TestAuditHandlerMapsRefusedFiltersOntoStatuses(t *testing.T) {
	t.Parallel()

	engine := auditEngine(t, &stubPanel{page: panelAuditPage()}, stubResolver{role: "owner"})
	badPage := doPackageRequest(t, engine, http.MethodGet, "/api/admin/audit?page_size=101", "", true)
	if badPage.Code != http.StatusBadRequest {
		t.Fatalf("page_size=101 = %d, want 400 (%s)", badPage.Code, badPage.Body.String())
	}

	refused := auditEngine(t, &stubPanel{
		listErr: &adminservice.AuditFilterError{Field: "action", Reason: "Aksi audit tidak dikenal."},
	}, stubResolver{role: "owner"})
	response := doPackageRequest(t, refused, http.MethodGet, "/api/admin/audit?action=nope", "", true)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown action = %d, want 422 (%s)", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Aksi audit tidak dikenal.") {
		t.Fatalf("refusal does not carry its reason: %s", response.Body.String())
	}
}

// TestAuditHandlerHidesAnUnexpectedFailure keeps internal text out of the response
// while the service still reports the cause upward.
func TestAuditHandlerHidesAnUnexpectedFailure(t *testing.T) {
	t.Parallel()

	engine := auditEngine(t, &stubPanel{listErr: errors.New("postgres is down")},
		stubResolver{role: "owner"})

	recorder := doPackageRequest(t, engine, http.MethodGet, "/api/admin/audit", "", true)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("failed read = %d, want 500", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "postgres") {
		t.Fatalf("the response leaks the failure: %s", recorder.Body.String())
	}
}

// errorCodeOf reads the envelope's stable code without depending on the integration
// suite's helper.
func errorCodeOf(t *testing.T, body []byte) string {
	t.Helper()

	var envelope httpx.ErrorResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error envelope %q: %v", body, err)
	}
	return envelope.Error.Code
}
