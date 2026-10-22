package integration

import (
	"net/http"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestCORSIsOnlyOnThePublicReads pins the scope ADR 0014 promises: the two
// Login Page reads are readable from the router's origin, nothing else is
// (a session-bearing GET must not inherit a wildcard origin).
func TestCORSIsOnlyOnThePublicReads(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	cases := map[string]string{
		"/api/packages":                         "*",
		"/api/active-period?phone=628123456789": "*",
		"/api/health":                           "",
	}

	for path, want := range cases {
		recorder := harness.Do(t, http.MethodGet, path, nil)
		if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Fatalf("%s Access-Control-Allow-Origin = %q, want %q", path, got, want)
		}
	}
}
