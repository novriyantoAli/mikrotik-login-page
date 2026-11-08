package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	admindto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	adminentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// adminAuditEnvelope is GET /api/admin/audit as the panel reads it.
type adminAuditEnvelope struct {
	Data       []admindto.AdminAuditEntryResponse `json:"data"`
	TotalCount int64                              `json:"total_count"`
	Page       int                                `json:"page"`
	PageSize   int                                `json:"page_size"`
}

// TestAdminAuditContract_FixtureMatchesDTO keeps the shared fixture, the DTO and the
// panel's zod schema one shape. It also pins the two payload shapes the reader must
// render: a catalog edit with before/after, and a reveal that names its Transaction.
func TestAdminAuditContract_FixtureMatchesDTO(t *testing.T) {
	raw := loadContract(t, "admin-audit.response.json")

	var envelope adminAuditEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("fixture does not decode into the audit DTO: %v", err)
	}
	if len(envelope.Data) != 2 {
		t.Fatalf("fixture carries %d rows, want 2", len(envelope.Data))
	}
	if envelope.TotalCount != 2 || envelope.Page != 1 || envelope.PageSize != 20 {
		t.Fatalf("fixture envelope = %+v", envelope)
	}

	edit := envelope.Data[0]
	if edit.Action != string(audit.ActionPackageUpdate) || edit.TargetType != string(audit.TargetPackage) {
		t.Fatalf("edit row = %+v, want a package update", edit)
	}
	price, ok := edit.Payload["price_idr"].(map[string]any)
	if !ok || price["before"] == nil || price["after"] == nil {
		t.Fatalf("edit payload = %#v, want the price before and after", edit.Payload)
	}

	reveal := envelope.Data[1]
	if reveal.Action != string(audit.ActionPINReveal) || reveal.TargetType != string(audit.TargetTransaction) {
		t.Fatalf("reveal row = %+v, want a transaction reveal", reveal)
	}
	if _, carriesPIN := reveal.Payload["pin"]; carriesPIN {
		t.Fatalf("reveal payload carries a PIN: %#v", reveal.Payload)
	}

	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal the audit envelope: %v", err)
	}
	assertSameJSONShape(t, decodeJSON(t, raw), decodeJSON(t, roundTrip), "$")
}

// listAuditTrail is the reader as an operator runs it.
func listAuditTrail(
	t *testing.T,
	harness *testutil.APIHarness,
	cookie *http.Cookie,
	query url.Values,
) (*httptestResponse, adminAuditEnvelope) {
	t.Helper()

	path := "/api/admin/audit"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	recorder := harness.DoRequest(t, http.MethodGet, path, nil, withCookie(cookie, ownerIP))

	envelope := adminAuditEnvelope{}
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode %s response %q: %v", path, recorder.Body.String(), err)
		}
	}
	return &httptestResponse{code: recorder.Code, body: recorder.Body.Bytes()}, envelope
}

// seedAuditRow writes one trail row directly. The reader tests need rows with
// chosen actors, actions and instants, which no single endpoint can produce.
func seedAuditRow(t *testing.T, harness *testutil.APIHarness, row *adminentity.AuditEntry) *adminentity.AuditEntry {
	t.Helper()

	if row.AdminEmail == "" {
		row.AdminEmail = "owner@example.test"
	}
	if row.Payload == nil {
		row.Payload = adminentity.Payload{}
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)
	}
	if err := harness.Postgres.Create(row).Error; err != nil {
		t.Fatalf("seed audit row %q: %v", row.Action, err)
	}
	return row
}

// auditReaderCookies signs in one admin per role, so a matrix cell can be tested
// without rebuilding the team in every subtest.
func auditReaderCookies(
	t *testing.T,
	harness *testutil.APIHarness,
) (map[adminentity.Role]*http.Cookie, map[adminentity.Role]string) {
	t.Helper()

	ownerCookie := panelOwner(t, harness)
	passwords := map[adminentity.Role]string{
		adminentity.RoleSupport: "kata-sandi-support-audit",
		adminentity.RoleAuditor: "kata-sandi-auditor-audit",
	}
	emails := map[adminentity.Role]string{
		adminentity.RoleSupport: "support-audit@example.test",
		adminentity.RoleAuditor: "auditor-audit@example.test",
	}

	cookies := map[adminentity.Role]*http.Cookie{adminentity.RoleOwner: ownerCookie}
	for _, role := range []adminentity.Role{adminentity.RoleSupport, adminentity.RoleAuditor} {
		if created := createAdminRow(t, harness, ownerCookie, emails[role], string(role),
			passwords[role], ownerIP); created.code != http.StatusCreated {
			t.Fatalf("create %s = %d (%s)", role, created.code, created.body)
		}
		cookies[role] = mustAdminSession(t, harness, emails[role], passwords[role], ownerIP)
	}
	return cookies, passwords
}

// TestAdminAuditReader_RoleMatrixAtTheHTTPBoundary is #38's first acceptance
// criterion: support is refused by the server, owner and auditor read, and no
// session is 401 rather than 403.
func TestAdminAuditReader_RoleMatrixAtTheHTTPBoundary(t *testing.T) {
	harness := newPanelHarness(t)
	cookies, _ := auditReaderCookies(t, harness)
	seedAuditRow(t, harness, &adminentity.AuditEntry{
		AdminEmail: "owner@example.test",
		Action:     audit.ActionPackageUpdate,
		TargetType: string(audit.TargetPackage),
		TargetID:   "1",
	})

	for _, role := range []adminentity.Role{adminentity.RoleOwner, adminentity.RoleAuditor} {
		response, envelope := listAuditTrail(t, harness, cookies[role], nil)
		if response.code != http.StatusOK {
			t.Fatalf("%s reads the trail = %d (%s)", role, response.code, response.body)
		}
		// The team this test just created left its own create rows, so the reader is
		// asserted to answer rather than to count a fixed number here.
		if len(envelope.Data) == 0 || envelope.TotalCount == 0 {
			t.Fatalf("%s read an empty trail", role)
		}
	}

	support, _ := listAuditTrail(t, harness, cookies[adminentity.RoleSupport], nil)
	if support.code != http.StatusForbidden {
		t.Fatalf("support reads the trail = %d, want 403 (%s)", support.code, support.body)
	}
	if code := errorCode(t, support.body); code != httpx.CodeForbidden {
		t.Fatalf("error code = %q, want %q", code, httpx.CodeForbidden)
	}

	if recorder := harness.Do(t, http.MethodGet, "/api/admin/audit", nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous read = %d, want 401", recorder.Code)
	}
}

// TestAdminAuditReader_FiltersNarrowTheTrail proves the three filters really filter,
// and that a day is the WIB day the panel renders rather than a UTC day.
func TestAdminAuditReader_FiltersNarrowTheTrail(t *testing.T) {
	harness := newPanelHarness(t)
	// Only the seeded rows matter here, so the reader reads as the owner the
	// bootstrap created: creating extra admins would leave trail rows of its own.
	cookie := panelOwner(t, harness)

	// 08:00 WIB on 23 October.
	seedAuditRow(t, harness, &adminentity.AuditEntry{
		AdminEmail: "owner@example.test",
		Action:     audit.ActionPackageUpdate,
		TargetType: string(audit.TargetPackage),
		TargetID:   "1",
		CreatedAt:  time.Date(2026, time.October, 23, 1, 0, 0, 0, time.UTC),
	})
	// 10:00 WIB on 23 October, another actor.
	seedAuditRow(t, harness, &adminentity.AuditEntry{
		AdminEmail: "auditor@example.test",
		Action:     audit.ActionPackageCreate,
		TargetType: string(audit.TargetPackage),
		TargetID:   "2",
		CreatedAt:  time.Date(2026, time.October, 23, 3, 0, 0, 0, time.UTC),
	})
	// 01:30 WIB on 24 October: the same UTC day as the rows above, a different WIB day.
	seedAuditRow(t, harness, &adminentity.AuditEntry{
		AdminEmail: "owner@example.test",
		Action:     audit.ActionPINReveal,
		TargetType: string(audit.TargetTransaction),
		TargetID:   "12",
		CreatedAt:  time.Date(2026, time.October, 23, 18, 30, 0, 0, time.UTC),
	})
	// 23:30 WIB on 22 October.
	seedAuditRow(t, harness, &adminentity.AuditEntry{
		AdminEmail: "support@example.test",
		Action:     audit.ActionAdminCreate,
		TargetType: string(audit.TargetAdmin),
		TargetID:   "4",
		CreatedAt:  time.Date(2026, time.October, 22, 16, 30, 0, 0, time.UTC),
	})

	cases := []struct {
		name  string
		query url.Values
		want  int
	}{
		{"every row", url.Values{}, 4},
		{"one action", url.Values{"action": {"pin.reveal"}}, 1},
		{"one actor, typed in another case", url.Values{"admin_email": {"OWNER@example.test"}}, 2},
		{"one WIB day", url.Values{"from": {"2026-10-23"}, "to": {"2026-10-23"}}, 2},
		{"a day before the newest row", url.Values{"to": {"2026-10-22"}}, 1},
		{"actor and action together", url.Values{
			"admin_email": {"owner@example.test"},
			"action":      {"package.update"},
		}, 1},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response, envelope := listAuditTrail(t, harness, cookie, testCase.query)
			if response.code != http.StatusOK {
				t.Fatalf("read = %d (%s)", response.code, response.body)
			}
			if len(envelope.Data) != testCase.want || envelope.TotalCount != int64(testCase.want) {
				t.Fatalf("rows = %d total = %d, want %d", len(envelope.Data), envelope.TotalCount, testCase.want)
			}
		})
	}
}

// TestAdminAuditReader_RefusesAnUnknownFilter keeps a typo from looking like an
// empty trail: the answer is 422, not a page with no rows.
func TestAdminAuditReader_RefusesAnUnknownFilter(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	for name, testCase := range map[string]struct {
		query url.Values
		want  int
	}{
		// A value the reader judges is its own 422, with a reason.
		"unknown action": {
			query: url.Values{"action": {"package.explode"}},
			want:  http.StatusUnprocessableEntity,
		},
		"malformed day": {
			query: url.Values{"from": {"23-10-2026"}},
			want:  http.StatusUnprocessableEntity,
		},
		"reversed range": {
			query: url.Values{"from": {"2026-10-25"}, "to": {"2026-10-24"}},
			want:  http.StatusUnprocessableEntity,
		},
		// A number outside the transport's range is the binding's 400.
		"page size too big": {
			query: url.Values{"page_size": {"101"}},
			want:  http.StatusBadRequest,
		},
	} {
		t.Run(name, func(t *testing.T) {
			response, _ := listAuditTrail(t, harness, cookie, testCase.query)
			if response.code != testCase.want {
				t.Fatalf("%s = %d, want %d (%s)", name, response.code, testCase.want, response.body)
			}
		})
	}
}

// TestAdminAuditReader_RevealRowNamesItsTargetAndNoPlaintext is #38's second
// acceptance criterion: a reveal is readable as `pin.reveal` with its Transaction,
// and the digits it opened never appear in the response.
func TestAdminAuditReader_RevealRowNamesItsTargetAndNoPlaintext(t *testing.T) {
	harness := newPanelHarness(t)
	cookies, _ := auditReaderCookies(t, harness)

	const revealedPIN = "654321"
	row := seedProvisionedWithPIN(t, harness, "6281200000801", revealedPIN)

	if response := revealPIN(t, harness, cookies[adminentity.RoleSupport], row.ID); response.code != http.StatusOK {
		t.Fatalf("reveal = %d (%s)", response.code, response.body)
	}

	response, envelope := listAuditTrail(t, harness, cookies[adminentity.RoleOwner],
		url.Values{"action": {"pin.reveal"}})
	if response.code != http.StatusOK {
		t.Fatalf("read the trail = %d (%s)", response.code, response.body)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("reveal rows = %d, want exactly 1", len(envelope.Data))
	}

	reveal := envelope.Data[0]
	if reveal.Action != string(audit.ActionPINReveal) {
		t.Fatalf("action = %q, want pin.reveal", reveal.Action)
	}
	if reveal.TargetType != string(audit.TargetTransaction) || reveal.TargetID != uintString(row.ID) {
		t.Fatalf("target = %q/%q, want transaction/%d", reveal.TargetType, reveal.TargetID, row.ID)
	}
	if reveal.Payload["result"] != "ok" || reveal.Payload["reference"] != row.Reference {
		t.Fatalf("payload = %#v, want the outcome and the Transaction reference", reveal.Payload)
	}
	if bytes.Contains(response.body, []byte(revealedPIN)) {
		t.Fatalf("the audit response carries the revealed PIN: %s", response.body)
	}
}

// TestAdminAuditReader_ShowsCatalogBeforeAndAfter is #38's catalog criterion,
// exercised through the writes that produce the rows rather than a hand-built
// payload: an edit reads as before/after, and a create has no before side to invent.
func TestAdminAuditReader_ShowsCatalogBeforeAndAfter(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	created := createPackageThroughPanel(t, harness, cookie, map[string]any{
		"code": "paket-audit", "name": "Paket Audit", "price_idr": 5000,
		"duration_minutes": 60, "rate_limit": "1M/1M",
	})
	path := "/api/admin/packages/" + uintString(created.ID)
	edited := harness.DoRequest(t, http.MethodPut, path, mustJSON(t, map[string]any{
		"name": "Paket Audit", "price_idr": 7500, "duration_minutes": 60,
		"rate_limit": "1M/1M", "version": created.Version,
	}), withCookie(cookie, ownerIP))
	if edited.Code != http.StatusOK {
		t.Fatalf("PUT %s = %d (%s)", path, edited.Code, edited.Body.String())
	}

	_, envelope := listAuditTrail(t, harness, cookie, nil)
	if len(envelope.Data) != 2 {
		t.Fatalf("rows = %d, want the create and the edit", len(envelope.Data))
	}

	// Newest first: the edit is the row the owner came to answer.
	edit := envelope.Data[0]
	price, ok := edit.Payload["price_idr"].(map[string]any)
	if !ok || price["before"] != float64(5000) || price["after"] != float64(7500) {
		t.Fatalf("edit price payload = %#v, want 5000→7500", edit.Payload["price_idr"])
	}

	create := envelope.Data[1]
	if create.Action != string(audit.ActionPackageCreate) {
		t.Fatalf("second row = %q, want package.create", create.Action)
	}
	if _, hasBefore := create.Payload["before"]; hasBefore {
		t.Fatalf("a create invented a before side: %#v", create.Payload)
	}
	after, ok := create.Payload["after"].(map[string]any)
	if !ok || after["code"] != "paket-audit" {
		t.Fatalf("create payload = %#v, want the values it was created with", create.Payload)
	}
}

// TestAdminAuditReader_PaginatesStablyOnEqualTimestamps is the tie-break rule: two
// rows of one transaction share created_at, and id keeps a later page from
// repeating one row and skipping another.
func TestAdminAuditReader_PaginatesStablyOnEqualTimestamps(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	sameInstant := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)
	seeded := make(map[uint]bool, 5)
	for index := 0; index < 5; index++ {
		row := seedAuditRow(t, harness, &adminentity.AuditEntry{
			AdminEmail: "owner@example.test",
			Action:     audit.ActionPackageUpdate,
			TargetType: string(audit.TargetPackage),
			TargetID:   fmt.Sprintf("%d", index+1),
			Payload:    adminentity.Payload{"note": fmt.Sprintf("row %d", index)},
			CreatedAt:  sameInstant,
		})
		seeded[row.ID] = true
	}

	seen := make(map[uint]bool, len(seeded))
	for page := 1; page <= 3; page++ {
		response, envelope := listAuditTrail(t, harness, cookie, url.Values{
			"page":      {fmt.Sprintf("%d", page)},
			"page_size": {"2"},
		})
		if response.code != http.StatusOK {
			t.Fatalf("page %d = %d (%s)", page, response.code, response.body)
		}
		if envelope.TotalCount != 5 || envelope.Page != page || envelope.PageSize != 2 {
			t.Fatalf("page %d envelope = %+v", page, envelope)
		}
		for _, row := range envelope.Data {
			if seen[row.ID] {
				t.Fatalf("row %d appeared on two pages", row.ID)
			}
			seen[row.ID] = true
			if !seeded[row.ID] {
				t.Fatalf("row %d is not one of the seeded rows", row.ID)
			}
		}
	}
	if len(seen) != len(seeded) {
		t.Fatalf("pages covered %d rows, want all %d", len(seen), len(seeded))
	}
}

// TestAdminAuditTrailHasNoWritePath is #38's last acceptance criterion: the router
// exposes one GET and nothing that could change or remove a row.
func TestAdminAuditTrailHasNoWritePath(t *testing.T) {
	harness := newPanelHarness(t)

	var methods []string
	for _, route := range harness.Engine.Routes() {
		if route.Path == "/api/admin/audit" {
			methods = append(methods, route.Method)
		}
	}

	if len(methods) != 1 || methods[0] != http.MethodGet {
		t.Fatalf("audit routes = %v, want exactly one GET", methods)
	}
}
