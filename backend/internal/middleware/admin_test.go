package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
)

// fakeResolver stands in for the auth service: the guard only needs to know what
// one token resolves to, and that is the whole surface under test.
type fakeResolver struct {
	identity *dto.SessionResponse
	err      error
}

func (r fakeResolver) Session(context.Context, string) (*dto.SessionResponse, error) {
	return r.identity, r.err
}

func guardSettings() middleware.AdminCookieSettings {
	return middleware.AdminCookieSettings{Name: "hotspot_admin", Secure: true, TTL: 12 * time.Hour}
}

// newGuardEngine mounts one route behind a guard with the given accepted roles.
func newGuardEngine(resolver middleware.AdminSessionResolver, roles ...entity.Role) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	guard := middleware.NewAdminGuard(resolver, guardSettings(), zap.NewNop())
	group := engine.Group("/api/admin", guard.Require(roles...))
	group.GET("/probe", func(c *gin.Context) {
		identity, ok := middleware.AdminIdentityFrom(c)
		if !ok {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.JSON(http.StatusOK, gin.H{"email": identity.Email, "role": identity.Role})
	})
	return engine
}

func probe(t *testing.T, engine *gin.Engine, cookieValue string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/api/admin/probe", nil)
	if cookieValue != "" {
		request.AddCookie(&http.Cookie{Name: "hotspot_admin", Value: cookieValue})
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

// TestGuardAnswers401OnlyForAMissingSession is the 401/403 split ADR 0024 §3 asks
// for: no live session is 401, and a stale cookie is cleared with the same path it
// was set with.
func TestAdminGuardAnswers401OnlyForAMissingSession(t *testing.T) {
	engine := newGuardEngine(fakeResolver{err: service.ErrUnauthenticated})

	missing := probe(t, engine, "")
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie = %d, want 401", missing.Code)
	}
	if !strings.Contains(missing.Body.String(), `"unauthorized"`) {
		t.Fatalf("no cookie body = %s, want the unauthorized code", missing.Body.String())
	}

	stale := probe(t, engine, "stale-token")
	if stale.Code != http.StatusUnauthorized {
		t.Fatalf("stale cookie = %d, want 401", stale.Code)
	}
	cleared := strings.Join(stale.Result().Header.Values("Set-Cookie"), "; ")
	for _, want := range []string{"hotspot_admin=", "Path=/api/admin", "SameSite=Strict", "HttpOnly"} {
		if !strings.Contains(cleared, want) {
			t.Fatalf("cleared cookie %q is missing %q", cleared, want)
		}
	}
	if !strings.Contains(cleared, "Max-Age=0") && !strings.Contains(cleared, "Max-Age=-1") {
		// gin renders a negative MaxAge as an already-expired cookie; either spelling
		// removes it, and the assertion names both so the test is not brittle.
		t.Fatalf("cleared cookie %q does not expire", cleared)
	}
}

// TestGuardReportsAResolverFailureAs500: a broken session lookup is not "signed
// out", because telling the operator to log in again would hide the outage.
func TestAdminGuardReportsAResolverFailureAs500(t *testing.T) {
	engine := newGuardEngine(fakeResolver{err: errors.New("postgres is down")})

	recorder := probe(t, engine, "some-token")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("resolver failure = %d, want 500", recorder.Code)
	}
}

// TestGuardEnforcesTheRoleMatrix is ADR 0024 §3 at the middleware boundary: the
// route names the roles it accepts, and every other role is a 403.
func TestAdminGuardEnforcesTheRoleMatrix(t *testing.T) {
	roles := []entity.Role{entity.RoleOwner, entity.RoleSupport, entity.RoleAuditor}

	cases := []struct {
		action   string
		accepted []entity.Role
	}{
		{"read the catalog", nil},
		{"write the catalog", []entity.Role{entity.RoleOwner}},
		{"read transactions", nil},
		{"reveal a PIN", []entity.Role{entity.RoleOwner, entity.RoleSupport}},
		{"read subscribers", nil},
		{"manage admins", []entity.Role{entity.RoleOwner}},
		{"read the audit log", []entity.Role{entity.RoleOwner, entity.RoleAuditor}},
		{"change own password", nil},
	}

	for _, testCase := range cases {
		for _, role := range roles {
			// An empty list means "any authenticated role", exactly like Require.
			accepted := len(testCase.accepted) == 0
			for _, allowed := range testCase.accepted {
				if allowed == role {
					accepted = true
				}
			}

			name := testCase.action + "/" + string(role)
			t.Run(name, func(t *testing.T) {
				engine := newGuardEngine(fakeResolver{identity: &dto.SessionResponse{
					AdminID: 3, Email: "panel@example.test", Role: string(role),
				}}, testCase.accepted...)

				recorder := probe(t, engine, "live-token")
				if accepted && recorder.Code != http.StatusOK {
					t.Fatalf("%s as %s = %d, want 200", testCase.action, role, recorder.Code)
				}
				if !accepted && recorder.Code != http.StatusForbidden {
					t.Fatalf("%s as %s = %d, want 403", testCase.action, role, recorder.Code)
				}
				if !accepted && strings.Contains(recorder.Body.String(), string(role)) {
					t.Fatal("a 403 body names the role it refused")
				}
			})
		}
	}
}
