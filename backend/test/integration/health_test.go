package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

func TestHealthEndpoint_ReportsEveryDependency(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	status, body := harness.GetJSON(t, "/api/health")
	data := body["data"].(map[string]any)

	if data["service"] != harness.Config.App.Name {
		t.Fatalf("service = %v, want %q", data["service"], harness.Config.App.Name)
	}
	if version, _ := data["version"].(string); version == "" {
		t.Fatal("version must be reported")
	}
	if _, err := time.Parse(time.RFC3339, data["checked_at"].(string)); err != nil {
		t.Fatalf("checked_at is not RFC 3339: %v", err)
	}

	checks, ok := data["checks"].(map[string]any)
	if !ok {
		t.Fatalf("checks missing from response: %v", data)
	}
	assertSameKeys(t, map[string]any{
		database.CheckPostgres:        nil,
		database.CheckFreeRADIUSMySQL: nil,
		database.CheckRedis:           nil,
	}, checks, "data.checks")

	assertCheckStatus(t, checks, database.CheckPostgres, dto.CheckUp)
	assertCheckStatus(t, checks, database.CheckRedis, dto.CheckUp)

	if harness.Dependencies.FreeRADIUSMySQLDSN != "" {
		assertCheckStatus(t, checks, database.CheckFreeRADIUSMySQL, dto.CheckUp)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 when every dependency is up", status)
		}
		if data["status"] != dto.StatusOK {
			t.Fatalf("overall status = %v, want %q", data["status"], dto.StatusOK)
		}
		return
	}

	// No FreeRADIUS MySQL on this machine: the endpoint must still answer, and
	// must report the missing dependency instead of failing the request.
	assertCheckStatus(t, checks, database.CheckFreeRADIUSMySQL, dto.CheckDown)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when a dependency is down", status)
	}
	if data["status"] != dto.StatusDegraded {
		t.Fatalf("overall status = %v, want %q", data["status"], dto.StatusDegraded)
	}
}

func TestHealthEndpoint_ReportsDegradedDependencyWithReason(t *testing.T) {
	harness := testutil.NewAPIHarness(t, testutil.WithFreeRADIUSMySQLDSN(""))

	status, body := harness.GetJSON(t, "/api/health")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", status)
	}

	data := body["data"].(map[string]any)
	if data["status"] != dto.StatusDegraded {
		t.Fatalf("overall status = %v, want %q", data["status"], dto.StatusDegraded)
	}

	checks := data["checks"].(map[string]any)
	assertCheckStatus(t, checks, database.CheckPostgres, dto.CheckUp)
	assertCheckStatus(t, checks, database.CheckRedis, dto.CheckUp)

	mySQL := checks[database.CheckFreeRADIUSMySQL].(map[string]any)
	if mySQL["status"] != dto.CheckDown {
		t.Fatalf("freeradius_mysql status = %v, want %q", mySQL["status"], dto.CheckDown)
	}
	assertFailureCategory(t, mySQL)
}

func TestHealthEndpoint_UsesInjectedClock(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	_, body := harness.GetJSON(t, "/api/health")
	data := body["data"].(map[string]any)

	want := harness.Doubles.Clock.Now().Format(time.RFC3339)
	if data["checked_at"] != want {
		t.Fatalf("checked_at = %v, want %q from the injected clock", data["checked_at"], want)
	}
}

// assertFailureCategory requires a down check to publish one of the stable
// categories instead of a driver message.
func assertFailureCategory(t *testing.T, check map[string]any) {
	t.Helper()

	reason, _ := check["error"].(string)
	switch reason {
	case dto.FailureTimeout, dto.FailureUnreachable, dto.FailureAuthFailed, dto.FailureUnavailable:
	default:
		t.Fatalf("check error = %q, want a stable failure category", reason)
	}
}

func assertCheckStatus(t *testing.T, checks map[string]any, name, want string) {
	t.Helper()

	check, ok := checks[name].(map[string]any)
	if !ok {
		t.Fatalf("check %q missing", name)
	}
	if check["status"] != want {
		t.Fatalf("%s status = %v, want %q (check: %v)", name, check["status"], want, check)
	}
}
