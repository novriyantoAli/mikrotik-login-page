package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// ownerCredentials are the pair `migrate` seeded this schema's owner with.
func ownerCredentials() (email, password string) {
	settings := testutil.AdminBootstrapEnv()
	return settings["ADMIN_EMAIL"], settings["ADMIN_PASSWORD"]
}

// generousAdminRateLimits keeps the panel's two login budgets out of the tests that
// are not about them. The counters live in the package's shared Redis, so a test
// that signs in a handful of times would otherwise spend another test's budget.
func generousAdminRateLimits(t *testing.T) {
	t.Helper()
	t.Setenv("ADMIN_RATE_LIMIT_IP_LIMIT", "1000")
	t.Setenv("ADMIN_RATE_LIMIT_EMAIL_LIMIT", "1000")
}

// adminSignIn signs in through the production router and returns the response plus
// the admin cookie it set (nil when the login was refused).
func adminSignIn(
	t *testing.T,
	harness *testutil.APIHarness,
	email, password, clientIP string,
) (*httptestResponse, *http.Cookie) {
	t.Helper()

	recorder := harness.DoRequest(t, http.MethodPost, "/api/admin/auth/login",
		mustJSON(t, map[string]any{"email": email, "password": password}),
		func(request *http.Request) { request.RemoteAddr = clientIP + ":54321" })

	response := &httptestResponse{code: recorder.Code, body: recorder.Body.Bytes()}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == harness.Config.Admin.CookieName {
			return response, cookie
		}
	}
	return response, nil
}

// mustAdminSession signs in and fails the test when the panel refuses, so a test
// about a different rule is not silently passing with no cookie.
func mustAdminSession(
	t *testing.T,
	harness *testutil.APIHarness,
	email, password, clientIP string,
) *http.Cookie {
	t.Helper()

	response, cookie := adminSignIn(t, harness, email, password, clientIP)
	if response.code != http.StatusOK || cookie == nil {
		t.Fatalf("admin sign-in = %d (%s)", response.code, response.body)
	}
	return cookie
}

// createAdminRow adds one panel actor through the panel's own endpoint.
func createAdminRow(
	t *testing.T,
	harness *testutil.APIHarness,
	ownerCookie *http.Cookie,
	email, role, password, clientIP string,
) *httptestResponse {
	t.Helper()

	recorder := harness.DoRequest(t, http.MethodPost, "/api/admin/admins",
		mustJSON(t, map[string]any{"email": email, "role": role, "password": password}),
		withCookie(ownerCookie, clientIP))
	return &httptestResponse{code: recorder.Code, body: recorder.Body.Bytes()}
}

// TestAdminLoginSetsItsOwnCookieAndLogoutRevokesIt is ADR 0024 §2 and §5 end to
// end: the cookie is HttpOnly, Strict, scoped to /api/admin and has the configured
// absolute lifetime, and signing out kills the row rather than only the browser's
// copy.
func TestAdminLoginSetsItsOwnCookieAndLogoutRevokesIt(t *testing.T) {
	generousAdminRateLimits(t)
	harness := testutil.NewAPIHarness(t)
	email, password := ownerCredentials()

	response, cookie := adminSignIn(t, harness, email, password, "203.0.113.21")
	if response.code != http.StatusOK {
		t.Fatalf("login = %d (%s)", response.code, response.body)
	}
	if cookie == nil {
		t.Fatal("login set no admin cookie")
	}

	attributes := cookie.String()
	for _, want := range []string{"Path=" + httpx.AdminCookiePath, "HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(attributes, want) {
			t.Fatalf("admin cookie %q is missing %q", attributes, want)
		}
	}
	if cookie.Name != harness.Config.Admin.CookieName {
		t.Fatalf("cookie name = %q, want the admin one %q", cookie.Name, harness.Config.Admin.CookieName)
	}
	// An absolute TTL: the browser is told the same lifetime the row has.
	if cookie.MaxAge < int(harness.Config.Admin.SessionTTL.Seconds())-1 ||
		cookie.MaxAge > int(harness.Config.Admin.SessionTTL.Seconds()) {
		t.Fatalf("cookie Max-Age = %d, want the absolute TTL %s",
			cookie.MaxAge, harness.Config.Admin.SessionTTL)
	}

	session := &httptestResponse{}
	{
		recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/auth/session", nil,
			withCookie(cookie, "203.0.113.21"))
		session.code, session.body = recorder.Code, recorder.Body.Bytes()
	}
	if session.code != http.StatusOK {
		t.Fatalf("session = %d (%s)", session.code, session.body)
	}
	body := session.decode(t)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("session body = %v, want a data envelope", body)
	}
	if data["email"] != email || data["role"] != string(entity.RoleOwner) {
		t.Fatalf("session data = %v, want %s/owner", data, email)
	}
	if _, present := data["id"]; present {
		t.Fatal("the session body exposes the admin id")
	}

	logout := harness.DoRequest(t, http.MethodPost, "/api/admin/auth/logout", nil,
		withCookie(cookie, "203.0.113.21"))
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d (%s)", logout.Code, logout.Body.String())
	}

	after := harness.DoRequest(t, http.MethodGet, "/api/admin/auth/session", nil,
		withCookie(cookie, "203.0.113.21"))
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d, want 401", after.Code)
	}

	// The row, not only the cookie: a stolen copy of the old value must fail too.
	var live int64
	if err := harness.Postgres.Model(&entity.Session{}).
		Where("revoked_at IS NULL").Count(&live).Error; err != nil {
		t.Fatalf("count live admin sessions: %v", err)
	}
	if live != 0 {
		t.Fatalf("live admin sessions after logout = %d, want 0", live)
	}
}

// TestAdminLoginAnswersEveryCredentialFailureIdentically is ADR 0024 §4 at the HTTP
// boundary: status and body are byte-for-byte the same, so the endpoint cannot be
// used to enumerate admins.
func TestAdminLoginAnswersEveryCredentialFailureIdentically(t *testing.T) {
	generousAdminRateLimits(t)
	harness := testutil.NewAPIHarness(t)
	email, password := ownerCredentials()

	unknown, unknownCookie := adminSignIn(t, harness, "ghost@example.test", password, "203.0.113.22")
	wrong, wrongCookie := adminSignIn(t, harness, email, "kata-sandi-yang-salah-sekali", "203.0.113.23")

	if unknown.code != http.StatusUnauthorized || wrong.code != http.StatusUnauthorized {
		t.Fatalf("statuses = %d/%d, want 401/401", unknown.code, wrong.code)
	}
	if string(unknown.body) != string(wrong.body) {
		t.Fatalf("bodies differ:\n unknown: %s\n wrong:   %s", unknown.body, wrong.body)
	}
	if unknownCookie != nil || wrongCookie != nil {
		t.Fatal("a refused login set an admin cookie")
	}
	if code := errorCode(t, unknown.body); code != httpx.CodeUnauthorized {
		t.Fatalf("error code = %q, want %q", code, httpx.CodeUnauthorized)
	}
}

// TestAdminLoginIsRateLimitedPerIPAndPerEmail is ADR 0024 §4's two dimensions: one
// address and one email each get their own budget, and exhausting either is 429.
func TestAdminLoginIsRateLimitedPerIPAndPerEmail(t *testing.T) {
	t.Setenv("ADMIN_RATE_LIMIT_IP_WINDOW", "5m")
	t.Setenv("ADMIN_RATE_LIMIT_IP_LIMIT", "2")
	t.Setenv("ADMIN_RATE_LIMIT_EMAIL_WINDOW", "5m")
	t.Setenv("ADMIN_RATE_LIMIT_EMAIL_LIMIT", "2")

	harness := testutil.NewAPIHarness(t)

	t.Run("per IP", func(t *testing.T) {
		const clientIP = "203.0.113.31"
		for attempt := 1; attempt <= 2; attempt++ {
			response, _ := adminSignIn(t, harness, "ghost-ip-"+uintString(uint(attempt))+"@example.test",
				"kata-sandi-yang-salah-sekali", clientIP)
			if response.code != http.StatusUnauthorized {
				t.Fatalf("attempt %d = %d, want 401", attempt, response.code)
			}
		}

		limited, _ := adminSignIn(t, harness, "ghost-ip-3@example.test", "kata-sandi-yang-salah-sekali", clientIP)
		if limited.code != http.StatusTooManyRequests {
			t.Fatalf("third attempt from one IP = %d, want 429", limited.code)
		}
		if code := errorCode(t, limited.body); code != httpx.CodeRateLimited {
			t.Fatalf("error code = %q, want %q", code, httpx.CodeRateLimited)
		}
	})

	t.Run("per email", func(t *testing.T) {
		const email = "budget@example.test"
		for attempt := 1; attempt <= 2; attempt++ {
			ip := "203.0.113." + uintString(uint(40+attempt))
			response, _ := adminSignIn(t, harness, email, "kata-sandi-yang-salah-sekali", ip)
			if response.code != http.StatusUnauthorized {
				t.Fatalf("attempt %d = %d, want 401", attempt, response.code)
			}
		}

		limited, _ := adminSignIn(t, harness, email, "kata-sandi-yang-salah-sekali", "203.0.113.44")
		if limited.code != http.StatusTooManyRequests {
			t.Fatalf("third attempt for one email = %d, want 429", limited.code)
		}
	})
}

// TestAdminSessionExpiresWithoutSliding is the absolute-TTL criterion: reading the
// session does not renew it, so a forgotten browser closes itself 12 hours after
// login and not 12 hours after its last request.
func TestAdminSessionExpiresWithoutSliding(t *testing.T) {
	generousAdminRateLimits(t)
	harness := testutil.NewAPIHarness(t)
	email, password := ownerCredentials()
	cookie := mustAdminSession(t, harness, email, password, "203.0.113.51")

	// A read one minute before the TTL still works and must not extend anything.
	harness.Doubles.Clock.Advance(12*time.Hour - time.Minute)
	if recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/auth/session", nil,
		withCookie(cookie, "203.0.113.51")); recorder.Code != http.StatusOK {
		t.Fatalf("session just before the TTL = %d, want 200", recorder.Code)
	}

	harness.Doubles.Clock.Advance(2 * time.Minute)
	if recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/auth/session", nil,
		withCookie(cookie, "203.0.113.51")); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("session past the TTL = %d, want 401", recorder.Code)
	}
}

// TestAdminPasswordRuleRefusesWeakAndEmailEcho pins the two rules ADR 0024 §1 names,
// through the HTTP code the panel branches on.
func TestAdminPasswordRuleRefusesWeakAndEmailEcho(t *testing.T) {
	generousAdminRateLimits(t)
	harness := testutil.NewAPIHarness(t)
	email, password := ownerCredentials()
	const clientIP = "203.0.113.61"
	cookie := mustAdminSession(t, harness, email, password, clientIP)

	for name, newPassword := range map[string]string{
		"shorter than twelve": "pendek",
		"the email itself":    email,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := harness.DoRequest(t, http.MethodPost, "/api/admin/auth/password",
				mustJSON(t, map[string]any{"current_password": password, "new_password": newPassword}),
				withCookie(cookie, clientIP))
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("weak password = %d, want 422 (%s)", recorder.Code, recorder.Body.String())
			}
		})
	}

	// A wrong current password is 422 too, and it changes nothing.
	wrongCurrent := harness.DoRequest(t, http.MethodPost, "/api/admin/auth/password",
		mustJSON(t, map[string]any{"current_password": "bukan-sandi-yang-benar", "new_password": "kata-sandi-baru-yang-panjang"}),
		withCookie(cookie, clientIP))
	if wrongCurrent.Code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong current password = %d, want 422", wrongCurrent.Code)
	}

	changed := harness.DoRequest(t, http.MethodPost, "/api/admin/auth/password",
		mustJSON(t, map[string]any{"current_password": password, "new_password": "kata-sandi-baru-yang-panjang"}),
		withCookie(cookie, clientIP))
	if changed.Code != http.StatusNoContent {
		t.Fatalf("change own password = %d (%s)", changed.Code, changed.Body.String())
	}

	if response, _ := adminSignIn(t, harness, email, password, "203.0.113.62"); response.code != http.StatusUnauthorized {
		t.Fatalf("old password still signs in: %d", response.code)
	}
	if _, newCookie := adminSignIn(t, harness, email, "kata-sandi-baru-yang-panjang", "203.0.113.63"); newCookie == nil {
		t.Fatal("the new password does not sign in")
	}
}

// TestAdminResetPasswordRevokesEverySession is ADR 0024 §5: after an owner resets a
// colleague's password, every token that colleague had is refused immediately.
func TestAdminResetPasswordRevokesEverySession(t *testing.T) {
	generousAdminRateLimits(t)
	harness := testutil.NewAPIHarness(t)
	ownerEmail, ownerPassword := ownerCredentials()
	const ownerIP = "203.0.113.71"
	ownerCookie := mustAdminSession(t, harness, ownerEmail, ownerPassword, ownerIP)

	const supportEmail = "support-reset@example.test"
	const supportPassword = "kata-sandi-support-yang-panjang"
	if created := createAdminRow(t, harness, ownerCookie, supportEmail, "support", supportPassword, ownerIP); created.code != http.StatusCreated {
		t.Fatalf("create support = %d (%s)", created.code, created.body)
	}

	first := mustAdminSession(t, harness, supportEmail, supportPassword, "203.0.113.72")
	second := mustAdminSession(t, harness, supportEmail, supportPassword, "203.0.113.73")

	target := readAdminByEmail(t, harness, supportEmail)
	reset := harness.DoRequest(t, http.MethodPost,
		"/api/admin/admins/"+uintString(target.ID)+"/password",
		mustJSON(t, map[string]any{"password": "kata-sandi-baru-yang-panjang"}),
		withCookie(ownerCookie, ownerIP))
	if reset.Code != http.StatusNoContent {
		t.Fatalf("reset password = %d (%s)", reset.Code, reset.Body.String())
	}

	for index, cookie := range []*http.Cookie{first, second} {
		recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/auth/session", nil,
			withCookie(cookie, "203.0.113.7"+uintString(uint(4+index))))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("session %d after reset = %d, want 401", index+1, recorder.Code)
		}
	}

	if response, _ := adminSignIn(t, harness, supportEmail, supportPassword, "203.0.113.76"); response.code != http.StatusUnauthorized {
		t.Fatalf("old password after reset = %d, want 401", response.code)
	}
	if _, cookie := adminSignIn(t, harness, supportEmail, "kata-sandi-baru-yang-panjang", "203.0.113.77"); cookie == nil {
		t.Fatal("the reset password does not sign in")
	}

	assertAuditAction(t, harness, audit.ActionAdminPasswordReset, ownerEmail)
}

// TestAdminDeactivationRevokesSessionsAndRefusesLogin is the other half of ADR 0024
// §5, plus its own consequence: a deactivated admin cannot sign in again.
func TestAdminDeactivationRevokesSessionsAndRefusesLogin(t *testing.T) {
	generousAdminRateLimits(t)
	harness := testutil.NewAPIHarness(t)
	ownerEmail, ownerPassword := ownerCredentials()
	const ownerIP = "203.0.113.81"
	ownerCookie := mustAdminSession(t, harness, ownerEmail, ownerPassword, ownerIP)

	const supportEmail = "support-off@example.test"
	const supportPassword = "kata-sandi-support-yang-panjang"
	createAdminRow(t, harness, ownerCookie, supportEmail, "support", supportPassword, ownerIP)
	supportCookie := mustAdminSession(t, harness, supportEmail, supportPassword, "203.0.113.82")

	target := readAdminByEmail(t, harness, supportEmail)
	deactivate := harness.DoRequest(t, http.MethodPost,
		"/api/admin/admins/"+uintString(target.ID)+"/active",
		mustJSON(t, map[string]any{"active": false}),
		withCookie(ownerCookie, ownerIP))
	if deactivate.Code != http.StatusOK {
		t.Fatalf("deactivate = %d (%s)", deactivate.Code, deactivate.Body.String())
	}

	if recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/auth/session", nil,
		withCookie(supportCookie, "203.0.113.83")); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("deactivated session = %d, want 401", recorder.Code)
	}
	if response, _ := adminSignIn(t, harness, supportEmail, supportPassword, "203.0.113.84"); response.code != http.StatusUnauthorized {
		t.Fatalf("deactivated login = %d, want 401", response.code)
	}

	assertAuditAction(t, harness, audit.ActionAdminDeactivate, ownerEmail)
}

// TestAdminRoleMatrixAtTheHTTPBoundary proves the 403s come from the backend for
// the cells ADR 0024 §3 can already exercise at this slice: managing admins is
// owner-only, and reading the session / changing your own password is every role.
func TestAdminRoleMatrixAtTheHTTPBoundary(t *testing.T) {
	generousAdminRateLimits(t)
	harness := testutil.NewAPIHarness(t)
	ownerEmail, ownerPassword := ownerCredentials()
	const ownerIP = "203.0.113.91"
	ownerCookie := mustAdminSession(t, harness, ownerEmail, ownerPassword, ownerIP)

	passwords := map[entity.Role]string{
		entity.RoleOwner:   ownerPassword,
		entity.RoleSupport: "kata-sandi-support-yang-panjang",
		entity.RoleAuditor: "kata-sandi-auditor-yang-panjang",
	}
	emails := map[entity.Role]string{
		entity.RoleOwner:   ownerEmail,
		entity.RoleSupport: "support-matrix@example.test",
		entity.RoleAuditor: "auditor-matrix@example.test",
	}
	for _, role := range []entity.Role{entity.RoleSupport, entity.RoleAuditor} {
		if created := createAdminRow(t, harness, ownerCookie, emails[role], string(role), passwords[role], ownerIP); created.code != http.StatusCreated {
			t.Fatalf("create %s = %d (%s)", role, created.code, created.body)
		}
	}

	cookies := map[entity.Role]*http.Cookie{
		entity.RoleOwner: mustAdminSession(t, harness, emails[entity.RoleOwner], passwords[entity.RoleOwner], ownerIP),
	}
	for index, role := range []entity.Role{entity.RoleSupport, entity.RoleAuditor} {
		cookies[role] = mustAdminSession(t, harness, emails[role], passwords[role],
			"203.0.113."+uintString(uint(92+index)))
	}

	for _, role := range []entity.Role{entity.RoleOwner, entity.RoleSupport, entity.RoleAuditor} {
		t.Run("read the session as "+string(role), func(t *testing.T) {
			recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/auth/session", nil,
				withCookie(cookies[role], "203.0.113.95"))
			if recorder.Code != http.StatusOK {
				t.Fatalf("session = %d, want 200 for every role", recorder.Code)
			}
		})

		t.Run("change own password as "+string(role), func(t *testing.T) {
			// A weak password reaches the service and answers 422; a 403 would mean
			// the guard refused a cell the matrix grants.
			recorder := harness.DoRequest(t, http.MethodPost, "/api/admin/auth/password",
				mustJSON(t, map[string]any{"current_password": passwords[role], "new_password": "pendek"}),
				withCookie(cookies[role], "203.0.113.96"))
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("change own password = %d, want 422 (%s)", recorder.Code, recorder.Body.String())
			}
		})

		t.Run("manage admins as "+string(role), func(t *testing.T) {
			recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/admins", nil,
				withCookie(cookies[role], "203.0.113.97"))
			if role == entity.RoleOwner {
				if recorder.Code != http.StatusOK {
					t.Fatalf("owner list admins = %d (%s)", recorder.Code, recorder.Body.String())
				}
				return
			}
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("%s list admins = %d, want 403", role, recorder.Code)
			}
			if code := errorCode(t, recorder.Body.Bytes()); code != httpx.CodeForbidden {
				t.Fatalf("error code = %q, want %q", code, httpx.CodeForbidden)
			}
		})
	}

	// Creating, resetting and deactivating are owner-only as well: each non-owner
	// role is refused before the body is even read.
	target := readAdminByEmail(t, harness, emails[entity.RoleAuditor])
	for _, role := range []entity.Role{entity.RoleSupport, entity.RoleAuditor} {
		for _, path := range []string{
			"/api/admin/admins",
			"/api/admin/admins/" + uintString(target.ID) + "/password",
			"/api/admin/admins/" + uintString(target.ID) + "/active",
		} {
			recorder := harness.DoRequest(t, http.MethodPost, path,
				mustJSON(t, map[string]any{"email": "x@example.test", "role": "support", "password": "kata-sandi-yang-cukup-panjang"}),
				withCookie(cookies[role], "203.0.113.98"))
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("%s POST %s = %d, want 403", role, path, recorder.Code)
			}
		}
	}
}

// TestAdminAndBuyerCookiesAreIsolated is the structural separation ADR 0024 §2
// asks for, proven in both directions at the HTTP boundary.
func TestAdminAndBuyerCookiesAreIsolated(t *testing.T) {
	generousAdminRateLimits(t)
	harness := testutil.NewAPIHarness(t)
	ownerEmail, ownerPassword := ownerCredentials()

	adminCookie := mustAdminSession(t, harness, ownerEmail, ownerPassword, "203.0.113.101")
	buyerCookie := newSessionCookie(t, harness, "081300000199", "203.0.113.199")

	// An admin cookie is not a buyer session…
	for _, path := range []string{"/api/account", "/api/transactions"} {
		recorder := harness.DoRequest(t, http.MethodGet, path, nil,
			withCookie(adminCookie, "203.0.113.101"))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s with the admin cookie = %d, want 401", path, recorder.Code)
		}
	}

	// …and a buyer cookie is not an admin session.
	recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/auth/session", nil,
		withCookie(buyerCookie, "203.0.113.199"))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("admin session with the buyer cookie = %d, want 401", recorder.Code)
	}

	// Sending the buyer's cookie under the admin cookie's name still fails: the two
	// tables are separate, not merely differently named.
	disguised := &http.Cookie{Name: harness.Config.Admin.CookieName, Value: buyerCookie.Value}
	if recorder := harness.DoRequest(t, http.MethodGet, "/api/admin/auth/session", nil,
		withCookie(disguised, "203.0.113.199")); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("buyer token under the admin cookie name = %d, want 401", recorder.Code)
	}
}

// readAdminByEmail loads one admin the tests just created, so an id comes from the
// database instead of from a response the test would otherwise have to parse.
func readAdminByEmail(t *testing.T, harness *testutil.APIHarness, email string) entity.Admin {
	t.Helper()

	var admin entity.Admin
	if err := harness.Postgres.Where("email = ?", email).First(&admin).Error; err != nil {
		t.Fatalf("read admin %q: %v", email, err)
	}
	return admin
}

// assertAuditAction proves the management write left exactly one trail row with the
// action ADR 0027 §3 names, attributed to the owner who performed it.
func assertAuditAction(
	t *testing.T,
	harness *testutil.APIHarness,
	action audit.Action,
	actorEmail string,
) {
	t.Helper()

	var rows []entity.AuditEntry
	if err := harness.Postgres.Where("action = ?", action).Find(&rows).Error; err != nil {
		t.Fatalf("read audit rows for %q: %v", action, err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit rows for %q = %d, want exactly 1", action, len(rows))
	}
	if rows[0].AdminEmail != actorEmail {
		t.Fatalf("audit actor = %q, want the signed-in %q", rows[0].AdminEmail, actorEmail)
	}
	if rows[0].TargetType != string(audit.TargetAdmin) {
		t.Fatalf("audit target type = %q, want %q", rows[0].TargetType, audit.TargetAdmin)
	}
}

// TestAdminManagementAnswersConflictNotFoundAndInvalidRole pins the error mapping
// the panel branches on: a taken email is 409, an unknown (or non-numeric) target
// is 404, and an unknown role is 422 — never a 500 and never a silent success.
func TestAdminManagementAnswersConflictNotFoundAndInvalidRole(t *testing.T) {
	generousAdminRateLimits(t)
	harness := testutil.NewAPIHarness(t)
	ownerEmail, ownerPassword := ownerCredentials()
	const ownerIP = "203.0.113.111"
	ownerCookie := mustAdminSession(t, harness, ownerEmail, ownerPassword, ownerIP)

	duplicate := createAdminRow(t, harness, ownerCookie, ownerEmail, "support", "kata-sandi-support-yang-panjang", ownerIP)
	if duplicate.code != http.StatusConflict {
		t.Fatalf("duplicate email = %d, want 409 (%s)", duplicate.code, duplicate.body)
	}
	if code := errorCode(t, duplicate.body); code != httpx.CodeConflict {
		t.Fatalf("error code = %q, want %q", code, httpx.CodeConflict)
	}

	badRole := createAdminRow(t, harness, ownerCookie, "role@example.test", "superuser", "kata-sandi-support-yang-panjang", ownerIP)
	if badRole.code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown role = %d, want 422 (%s)", badRole.code, badRole.body)
	}

	for name, id := range map[string]string{
		"unknown id":     "999999",
		"non-numeric id": "not-a-number",
	} {
		t.Run(name+" reset", func(t *testing.T) {
			recorder := harness.DoRequest(t, http.MethodPost, "/api/admin/admins/"+id+"/password",
				mustJSON(t, map[string]any{"password": "kata-sandi-baru-yang-panjang"}),
				withCookie(ownerCookie, ownerIP))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("reset %s = %d, want 404", name, recorder.Code)
			}
		})

		t.Run(name+" active", func(t *testing.T) {
			recorder := harness.DoRequest(t, http.MethodPost, "/api/admin/admins/"+id+"/active",
				mustJSON(t, map[string]any{"active": false}),
				withCookie(ownerCookie, ownerIP))
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("set active %s = %d, want 404", name, recorder.Code)
			}
		})
	}

	// Nothing above wrote a trail row: a refused management write leaves nothing.
	var rows int64
	if err := harness.Postgres.Model(&entity.AuditEntry{}).Count(&rows).Error; err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("audit rows after refused writes = %d, want 0", rows)
	}
}
