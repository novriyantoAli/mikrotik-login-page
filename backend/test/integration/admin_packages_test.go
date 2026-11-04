package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	adminentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	packagesdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	transactionentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

const ownerIP = "203.0.113.120"

// newPanelHarness builds a harness whose login budgets are wide enough for the
// repeated sign-ins these tests make. The env has to be set before the graph reads
// its configuration, which is why it is not inside panelOwner.
func newPanelHarness(t *testing.T) *testutil.APIHarness {
	t.Helper()

	generousAdminRateLimits(t)
	return testutil.NewAPIHarness(t)
}

// panelOwner signs the seeded owner in and returns the cookie every catalog write
// test acts with.
func panelOwner(t *testing.T, harness *testutil.APIHarness) *http.Cookie {
	t.Helper()

	email, password := ownerCredentials()
	return mustAdminSession(t, harness, email, password, ownerIP)
}

func decodeAdminPackage(t *testing.T, body []byte) packagesdto.AdminPackageResponse {
	t.Helper()

	var envelope struct {
		Data packagesdto.AdminPackageResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode package %q: %v", body, err)
	}
	return envelope.Data
}

func listAdminPackages(
	t *testing.T,
	harness *testutil.APIHarness,
	cookie *http.Cookie,
) []packagesdto.AdminPackageResponse {
	t.Helper()

	recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/packages", nil, withCookie(cookie, ownerIP))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/packages = %d (%s)", recorder.Code, recorder.Body.String())
	}

	var envelope struct {
		Data []packagesdto.AdminPackageResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	return envelope.Data
}

func createPackageThroughPanel(
	t *testing.T,
	harness *testutil.APIHarness,
	cookie *http.Cookie,
	payload map[string]any,
) packagesdto.AdminPackageResponse {
	t.Helper()

	recorder := harness.DoRequest(t, http.MethodPost, "/api/admin/packages", mustJSON(t, payload),
		withCookie(cookie, ownerIP))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /api/admin/packages = %d (%s)", recorder.Code, recorder.Body.String())
	}
	return decodeAdminPackage(t, recorder.Body.Bytes())
}

func auditRowsFor(t *testing.T, harness *testutil.APIHarness, action audit.Action) []adminentity.AuditEntry {
	t.Helper()

	var rows []adminentity.AuditEntry
	if err := harness.Postgres.Where("action = ?", action).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("read audit rows for %q: %v", action, err)
	}
	return rows
}

// TestAdminPackageWriteIsOwnerOnlyAtTheHTTPBoundary is the catalog's column of
// ADR 0024 §3: every role reads, only an owner writes, and no session is 401
// (never 403).
func TestAdminPackageWriteIsOwnerOnlyAtTheHTTPBoundary(t *testing.T) {
	harness := newPanelHarness(t)
	ownerCookie := panelOwner(t, harness)

	passwords := map[adminentity.Role]string{
		adminentity.RoleOwner:   "",
		adminentity.RoleSupport: "kata-sandi-support-yang-panjang",
		adminentity.RoleAuditor: "kata-sandi-auditor-yang-panjang",
	}
	emails := map[adminentity.Role]string{
		adminentity.RoleOwner:   ownerCredentialsEmail(),
		adminentity.RoleSupport: "support-katalog@example.test",
		adminentity.RoleAuditor: "auditor-katalog@example.test",
	}
	_, ownerPassword := ownerCredentials()
	passwords[adminentity.RoleOwner] = ownerPassword

	for _, role := range []adminentity.Role{adminentity.RoleSupport, adminentity.RoleAuditor} {
		if created := createAdminRow(t, harness, ownerCookie, emails[role], string(role), passwords[role],
			ownerIP); created.code != http.StatusCreated {
			t.Fatalf("create %s = %d (%s)", role, created.code, created.body)
		}
	}

	target := listAdminPackages(t, harness, ownerCookie)[0]
	for _, role := range []adminentity.Role{adminentity.RoleOwner, adminentity.RoleSupport, adminentity.RoleAuditor} {
		cookie := ownerCookie
		if role != adminentity.RoleOwner {
			cookie = mustAdminSession(t, harness, emails[role], passwords[role], ownerIP)
		}

		read := harness.DoRequest(t, http.MethodGet, "/api/admin/packages", nil, withCookie(cookie, ownerIP))
		if read.Code != http.StatusOK {
			t.Fatalf("%s reads the catalog = %d, want 200", role, read.Code)
		}
		if role == adminentity.RoleOwner {
			continue
		}

		for _, attempt := range []struct {
			method string
			path   string
			body   map[string]any
		}{
			{http.MethodPost, "/api/admin/packages", map[string]any{
				"code": "ditolak", "name": "Ditolak", "price_idr": 1000,
				"duration_minutes": 60, "rate_limit": "1M/1M",
			}},
			{http.MethodPut, "/api/admin/packages/" + uintString(target.ID), map[string]any{
				"name": "Ditolak", "price_idr": 1000, "duration_minutes": 60,
				"rate_limit": "1M/1M", "version": target.Version,
			}},
			{
				http.MethodPost, "/api/admin/packages/" + uintString(target.ID) + "/active",
				map[string]any{"active": false},
			},
		} {
			recorder := harness.DoRequest(t, attempt.method, attempt.path, mustJSON(t, attempt.body),
				withCookie(cookie, ownerIP))
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("%s %s %s = %d, want 403", role, attempt.method, attempt.path, recorder.Code)
			}
			if code := errorCode(t, recorder.Body.Bytes()); code != httpx.CodeForbidden {
				t.Fatalf("error code = %q, want %q", code, httpx.CodeForbidden)
			}
		}
	}

	for _, attempt := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/packages"},
		{http.MethodPost, "/api/admin/packages"},
		{http.MethodPost, "/api/admin/packages/" + uintString(target.ID) + "/active"},
	} {
		if recorder := harness.Do(t, attempt.method, attempt.path, nil); recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without a session = %d, want 401", attempt.method, attempt.path, recorder.Code)
		}
	}
}

// TestAdminPackageCreateUpdateVersionAndAudit walks the acceptance path: create,
// edit, stale edit, immutable code, and exactly one trail row per accepted write.
func TestAdminPackageCreateUpdateVersionAndAudit(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	created := createPackageThroughPanel(t, harness, cookie, map[string]any{
		"code": "paket-malam", "name": "  Paket Malam  ", "price_idr": 7000,
		"duration_minutes": 360, "rate_limit": "5M/5M",
	})
	if created.Code != "paket-malam" || created.Name != "Paket Malam" {
		t.Fatalf("created identity = %q/%q, want trimmed input", created.Code, created.Name)
	}
	if created.PriceIDR != 7000 || created.DurationMinutes != 360 || created.RateLimit != "5M/5M" {
		t.Fatalf("created values = %+v", created)
	}
	if !created.IsActive || created.Version != 1 {
		t.Fatalf("created package is_active=%v version=%d, want true/1", created.IsActive, created.Version)
	}

	// The public catalog shows the new Package; its frozen shape is untouched.
	status, public := harness.GetJSON(t, "/api/packages")
	if status != http.StatusOK {
		t.Fatalf("GET /api/packages = %d", status)
	}
	if !publicCatalogHasName(t, public, "Paket Malam") {
		t.Fatalf("the public catalog does not list the new Package: %v", public["data"])
	}

	path := "/api/admin/packages/" + uintString(created.ID)
	updated := harness.DoRequest(t, http.MethodPut, path, mustJSON(t, map[string]any{
		"code": "paket-malam", "name": "Paket Malam", "price_idr": 8000,
		"duration_minutes": 360, "rate_limit": "5M/5M", "version": created.Version,
	}), withCookie(cookie, ownerIP))
	if updated.Code != http.StatusOK {
		t.Fatalf("PUT %s = %d (%s)", path, updated.Code, updated.Body.String())
	}
	edited := decodeAdminPackage(t, updated.Body.Bytes())
	if edited.Version != 2 || edited.PriceIDR != 8000 {
		t.Fatalf("edited package = %+v, want version 2 at 8000", edited)
	}

	// A second write with the version the panel already used is refused and carries
	// the current row, so the panel can show what changed.
	stale := harness.DoRequest(t, http.MethodPut, path, mustJSON(t, map[string]any{
		"name": "Paket Malam", "price_idr": 9000, "duration_minutes": 360,
		"rate_limit": "5M/5M", "version": created.Version,
	}), withCookie(cookie, ownerIP))
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale PUT = %d, want 409 (%s)", stale.Code, stale.Body.String())
	}
	current := decodeConflictCurrent(t, stale.Body.Bytes())
	if current.Version != 2 || current.PriceIDR != 8000 {
		t.Fatalf("conflict carried %+v, want the current version 2 at 8000", current)
	}

	// The code is the seed's identity and cannot be renamed.
	rename := harness.DoRequest(t, http.MethodPut, path, mustJSON(t, map[string]any{
		"code": "paket-malam-baru", "name": "Paket Malam", "price_idr": 8000,
		"duration_minutes": 360, "rate_limit": "5M/5M", "version": 2,
	}), withCookie(cookie, ownerIP))
	if rename.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code change = %d, want 422 (%s)", rename.Code, rename.Body.String())
	}

	var stored packagesentity.Package
	if err := harness.Postgres.Where("code = ?", "paket-malam").First(&stored).Error; err != nil {
		t.Fatalf("read the stored package: %v", err)
	}
	if stored.Version != 2 || stored.PriceIDR != 8000 {
		t.Fatalf("stored row = %+v, want the accepted edit only", stored)
	}

	createdRows := auditRowsFor(t, harness, audit.ActionPackageCreate)
	if len(createdRows) != 1 || createdRows[0].TargetID != uintString(created.ID) {
		t.Fatalf("create audit rows = %+v, want one for package %d", createdRows, created.ID)
	}
	updatedRows := auditRowsFor(t, harness, audit.ActionPackageUpdate)
	if len(updatedRows) != 1 {
		t.Fatalf("update audit rows = %d, want exactly 1 (refused writes leave none)", len(updatedRows))
	}
	priceChange, ok := updatedRows[0].Payload["price_idr"].(map[string]any)
	if !ok {
		t.Fatalf("update payload lost the price: %+v", updatedRows[0].Payload)
	}
	if priceChange["before"] != float64(7000) || priceChange["after"] != float64(8000) {
		t.Fatalf("audited price change = %+v, want 7000→8000", priceChange)
	}
}

// TestAdminPackageRetirementKeepsTheRowAndThePublicContract proves a retired
// Package disappears from the Login Page's list but not from the panel's, and that
// neither list's shape changes.
func TestAdminPackageRetirementKeepsTheRowAndThePublicContract(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)
	target := listAdminPackages(t, harness, cookie)[0]

	path := "/api/admin/packages/" + uintString(target.ID) + "/active"
	retired := harness.DoRequest(t, http.MethodPost, path, mustJSON(t, map[string]any{"active": false}),
		withCookie(cookie, ownerIP))
	if retired.Code != http.StatusOK {
		t.Fatalf("retire = %d (%s)", retired.Code, retired.Body.String())
	}
	retiredRow := decodeAdminPackage(t, retired.Body.Bytes())
	if retiredRow.IsActive || retiredRow.Version != target.Version+1 {
		t.Fatalf("retired row = %+v, want inactive at version %d", retiredRow, target.Version+1)
	}

	status, public := harness.GetJSON(t, "/api/packages")
	if status != http.StatusOK {
		t.Fatalf("GET /api/packages = %d", status)
	}
	if publicCatalogHasName(t, public, target.Name) {
		t.Fatalf("the public catalog still lists the retired Package %q", target.Name)
	}

	adminList := listAdminPackages(t, harness, cookie)
	found := false
	for _, row := range adminList {
		if row.ID == target.ID {
			found = true
			if row.IsActive {
				t.Fatalf("the panel lost the retirement: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("the panel list must keep a retired Package")
	}

	revived := harness.DoRequest(t, http.MethodPost, path, mustJSON(t, map[string]any{"active": true}),
		withCookie(cookie, ownerIP))
	if revived.Code != http.StatusOK {
		t.Fatalf("revive = %d (%s)", revived.Code, revived.Body.String())
	}
	_, public = harness.GetJSON(t, "/api/packages")
	if !publicCatalogHasName(t, public, target.Name) {
		t.Fatalf("the revived Package %q is still missing from the public catalog", target.Name)
	}

	if rows := auditRowsFor(t, harness, audit.ActionPackageDeactivate); len(rows) != 1 {
		t.Fatalf("deactivate audit rows = %d, want 1", len(rows))
	}
	if rows := auditRowsFor(t, harness, audit.ActionPackageActivate); len(rows) != 1 {
		t.Fatalf("activate audit rows = %d, want 1", len(rows))
	}
}

// TestAdminPackageRefusesToRetireTheLastActivePackage is ADR 0025 §4: the Login
// Page must never render an empty catalog.
func TestAdminPackageRefusesToRetireTheLastActivePackage(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	catalog := listAdminPackages(t, harness, cookie)
	for _, row := range catalog[:len(catalog)-1] {
		path := "/api/admin/packages/" + uintString(row.ID) + "/active"
		recorder := harness.DoRequest(t, http.MethodPost, path, mustJSON(t, map[string]any{"active": false}),
			withCookie(cookie, ownerIP))
		if recorder.Code != http.StatusOK {
			t.Fatalf("retire %d = %d (%s)", row.ID, recorder.Code, recorder.Body.String())
		}
	}

	last := catalog[len(catalog)-1]
	path := "/api/admin/packages/" + uintString(last.ID) + "/active"
	refused := harness.DoRequest(t, http.MethodPost, path, mustJSON(t, map[string]any{"active": false}),
		withCookie(cookie, ownerIP))
	if refused.Code != http.StatusConflict {
		t.Fatalf("retiring the last active Package = %d, want 409 (%s)", refused.Code, refused.Body.String())
	}
	if code := errorCode(t, refused.Body.Bytes()); code != httpx.CodeConflict {
		t.Fatalf("error code = %q, want %q", code, httpx.CodeConflict)
	}

	var stored packagesentity.Package
	if err := harness.Postgres.First(&stored, last.ID).Error; err != nil {
		t.Fatalf("read the last Package: %v", err)
	}
	if !stored.IsActive {
		t.Fatal("the refused write retired the last active Package")
	}
}

// TestDeactivatingAPackageKeepsAPendingTransaction is ADR 0025 §3: the panel can
// stop selling, it cannot stop honoring. The checkout that was already open is
// paid and provisioned with the terms it bought.
func TestDeactivatingAPackageKeepsAPendingTransaction(t *testing.T) {
	harness := newPanelHarness(t)
	buyer := newSessionCookie(t, harness, "081300000301", "198.51.100.301")
	packageID := firstPackageID(t, harness)
	pending := checkout(t, harness, buyer, packageID, "BRIVA")

	cookie := panelOwner(t, harness)
	retire := harness.DoRequest(t, http.MethodPost,
		"/api/admin/packages/"+uintString(packageID)+"/active",
		mustJSON(t, map[string]any{"active": false}), withCookie(cookie, ownerIP))
	if retire.Code != http.StatusOK {
		t.Fatalf("retire the Package mid-checkout = %d (%s)", retire.Code, retire.Body.String())
	}

	if state := getTransaction(t, harness, buyer, pending.ID).State; state != string(transactionentity.StatePending) {
		t.Fatalf("retiring the Package moved the pending Transaction to %q", state)
	}

	if code := postCallback(t, harness, pending.Reference, payment.StatusPaid, pending.AmountIDR,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("settle after retirement = %d", code)
	}
	if err := harness.TransactionProvisioning.Provision(context.Background(), pending.ID); err != nil {
		t.Fatalf("Provision after retirement: %v", err)
	}
	if accounts := harness.Doubles.Radius.Accounts(); len(accounts) != 1 {
		t.Fatalf("provisioning applied %d accounts, want the pending purchase honored", len(accounts))
	}
}

// TestMagicLinkRefusesARetiredPackage pins the one cross-domain consequence of
// the public filter: a Magic Link may not carry an intent to buy a Package that is
// no longer on sale, while an active one still issues normally (ADR 0025 §1).
func TestMagicLinkRefusesARetiredPackage(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)
	catalog := listAdminPackages(t, harness, cookie)
	retired, onSale := catalog[0], catalog[1]

	retire := harness.DoRequest(t, http.MethodPost,
		"/api/admin/packages/"+uintString(retired.ID)+"/active",
		mustJSON(t, map[string]any{"active": false}), withCookie(cookie, ownerIP))
	if retire.Code != http.StatusOK {
		t.Fatalf("retire = %d (%s)", retire.Code, retire.Body.String())
	}

	refused := requestMagicLink(t, harness,
		map[string]any{"phone_number": "081300000302", "package_id": retired.ID}, "198.51.100.302")
	if refused.code != http.StatusBadRequest {
		t.Fatalf("magic link for a retired Package = %d, want 400 (%s)", refused.code, refused.body)
	}

	accepted := requestMagicLink(t, harness,
		map[string]any{"phone_number": "081300000303", "package_id": onSale.ID}, "198.51.100.303")
	if accepted.code != http.StatusAccepted {
		t.Fatalf("magic link for an active Package = %d, want 202 (%s)", accepted.code, accepted.body)
	}
}

// TestAdminPackagesContract_FixtureMatchesDTO proves the panel's list cannot grow
// or lose a field without the shared fixture changing with it.
func TestAdminPackagesContract_FixtureMatchesDTO(t *testing.T) {
	harness := newPanelHarness(t)
	cookie := panelOwner(t, harness)

	live := listAdminPackages(t, harness, cookie)
	if len(live) == 0 {
		t.Fatal("the panel catalog is empty; the seed should have run")
	}
	encoded, err := json.Marshal(live[0])
	if err != nil {
		t.Fatalf("marshal admin package: %v", err)
	}

	fixture := decodeJSON(t, loadContract(t, "admin-packages.response.json")).(map[string]any)
	fixturePackage := fixture["data"].([]any)[0].(map[string]any)
	assertSameKeys(t, fixturePackage, decodeJSON(t, encoded).(map[string]any), "$.data[]")
}

// publicCatalogHasName searches the frozen public envelope for one Package name.
func publicCatalogHasName(t *testing.T, body map[string]any, name string) bool {
	t.Helper()

	entries, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data is not an array: %T", body["data"])
	}
	for _, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("catalog entry is not an object: %T", entry)
		}
		if fields["nama"] == name {
			return true
		}
	}
	return false
}

// decodeConflictCurrent reads the row a stale-version 409 carries.
func decodeConflictCurrent(t *testing.T, body []byte) packagesdto.AdminPackageResponse {
	t.Helper()

	var envelope struct {
		Error struct {
			Current packagesdto.AdminPackageResponse `json:"current"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode conflict %q: %v", body, err)
	}
	if envelope.Error.Current.ID == 0 {
		t.Fatalf("a 409 must carry the current row: %s", body)
	}
	return envelope.Error.Current
}

func ownerCredentialsEmail() string {
	email, _ := ownerCredentials()
	return email
}
