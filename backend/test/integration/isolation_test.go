package integration

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/httpx"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// publicRoutes are the only routes that do not answer for one buyer: the two
// Login Page reads, the health check, the signed gateway callback, and the two
// steps of the Magic Link itself (the token is the credential there, ADR 0011).
var publicRoutes = []string{
	"GET /api/packages",
	"GET /api/active-period",
	"GET /api/health",
	"POST /api/payments/callback",
	"POST /api/auth/magic-link",
	"POST /api/auth/magic-link/exchange",
}

// isolationCase is one row of the table: the request a buyer could send naming
// somebody else, and the status the API must answer.
type isolationCase struct {
	name   string
	method string
	path   string
	body   map[string]any
	want   int
	code   string
}

// TestSessionEndpointsIsolatePhoneNumbers is #17's acceptance table. Every
// session-scoped endpoint appears here with a foreign Phone Number, so a new
// endpoint cannot ship without proving it cannot read or change another buyer's
// data (PRD point 13).
func TestSessionEndpointsIsolatePhoneNumbers(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	packageID := firstPackageID(t, harness)

	// Two real buyers, each with their own session and Transaction.
	ownCookie := newSessionCookie(t, harness, "081300000061", "198.51.100.61")
	foreignCookie := newSessionCookie(t, harness, "081300000062", "198.51.100.62")
	foreignTransaction := checkout(t, harness, foreignCookie, packageID, "BRIVA")

	ownPhone := "6281300000061"
	foreignPhone := "6281300000062"
	clientIP := "198.51.100.61"

	t.Run("table covers every session route", func(t *testing.T) {
		assertIsolationTableCoversEverySessionRoute(t, harness.Engine)
	})

	cases := []isolationCase{
		{
			name:   "account summary names a foreign Phone Number",
			method: http.MethodGet,
			path:   "/api/account?phone_number=" + foreignPhone,
			want:   http.StatusNotFound,
			code:   httpx.CodeNotFound,
		},
		{
			name:   "extend preview names a foreign Phone Number",
			method: http.MethodGet,
			path: "/api/account/extend-preview?package_id=" + uintString(packageID) +
				"&phone_number=" + foreignPhone,
			want: http.StatusNotFound,
			code: httpx.CodeNotFound,
		},
		{
			name:   "history names a foreign Phone Number",
			method: http.MethodGet,
			path:   "/api/transactions?phone_number=" + foreignPhone,
			want:   http.StatusNotFound,
			code:   httpx.CodeNotFound,
		},
		{
			name:   "checkout body names a foreign Phone Number",
			method: http.MethodPost,
			path:   "/api/transactions",
			body: map[string]any{
				"package_id": packageID, "channel": "BRIVA", "phone_number": foreignPhone,
			},
			want: http.StatusNotFound,
			code: httpx.CodeNotFound,
		},
		{
			name:   "another buyer's Transaction id",
			method: http.MethodGet,
			path:   "/api/transactions/" + uintString(foreignTransaction.ID),
			want:   http.StatusNotFound,
			code:   httpx.CodeNotFound,
		},
	}

	// The same requests naming the session's own Phone Number succeed, so a table
	// of only 404s cannot pass because an endpoint is simply broken.
	controls := []isolationCase{
		{
			name:   "account summary names the session's own Phone Number",
			method: http.MethodGet,
			path:   "/api/account?phone_number=" + ownPhone,
			want:   http.StatusOK,
		},
		{
			name:   "extend preview names the session's own Phone Number",
			method: http.MethodGet,
			path: "/api/account/extend-preview?package_id=" + uintString(packageID) +
				"&phone_number=" + ownPhone,
			want: http.StatusOK,
		},
		{
			name:   "history names the session's own Phone Number",
			method: http.MethodGet,
			path:   "/api/transactions?phone_number=" + ownPhone,
			want:   http.StatusOK,
		},
		{
			name:   "checkout body names the session's own Phone Number",
			method: http.MethodPost,
			path:   "/api/transactions",
			body: map[string]any{
				"package_id": packageID, "channel": "BRIVA", "phone_number": ownPhone,
			},
			want: http.StatusOK,
		},
	}

	for _, testCase := range append(cases, controls...) {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := harness.DoRequest(t, testCase.method, testCase.path,
				marshalBody(t, testCase.body), withCookie(ownCookie, clientIP))

			if recorder.Code != testCase.want {
				t.Fatalf("%s %s = %d, want %d (%s)",
					testCase.method, testCase.path, recorder.Code, testCase.want,
					recorder.Body.String())
			}
			if testCase.code != "" {
				if code := errorCode(t, recorder.Body.Bytes()); code != testCase.code {
					t.Fatalf("error code = %q, want %q", code, testCase.code)
				}
			}
			// A 404 must not carry the foreign identity back to the caller.
			if strings.Contains(recorder.Body.String(), foreignPhone) {
				t.Fatalf("the response echoes the other Phone Number: %s", recorder.Body.String())
			}
		})
	}

	t.Run("session reports only its own identity", func(t *testing.T) {
		recorder := harness.DoRequest(t, http.MethodGet,
			"/api/auth/session?phone_number="+foreignPhone, nil, withCookie(ownCookie, clientIP))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET /api/auth/session = %d (%s)", recorder.Code, recorder.Body.String())
		}

		var body struct {
			Data struct {
				PhoneNumber string `json:"phone_number"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode session: %v", err)
		}
		if body.Data.PhoneNumber != ownPhone {
			t.Fatalf("session phone_number = %q, want the cookie's %q",
				body.Data.PhoneNumber, ownPhone)
		}
	})

	t.Run("channels carry nothing owned by a buyer", func(t *testing.T) {
		// A channel list is shared vocabulary, not buyer data: naming a foreign
		// Phone Number must neither fail nor widen what comes back.
		recorder := harness.DoRequest(t, http.MethodGet,
			"/api/transactions/channels?phone_number="+foreignPhone, nil, withCookie(ownCookie, clientIP))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET /api/transactions/channels = %d (%s)", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), foreignPhone) ||
			strings.Contains(recorder.Body.String(), ownPhone) {
			t.Fatalf("a channel list carries a Phone Number: %s", recorder.Body.String())
		}
	})

	t.Run("logging out one buyer leaves the other signed in", func(t *testing.T) {
		if recorder := harness.DoRequest(t, http.MethodPost, "/api/auth/logout", nil,
			withCookie(foreignCookie, "198.51.100.62")); recorder.Code != http.StatusNoContent {
			t.Fatalf("foreign logout = %d (%s)", recorder.Code, recorder.Body.String())
		}

		// The buyer who logged out is gone…
		if recorder := harness.DoRequest(t, http.MethodGet, "/api/account", nil,
			withCookie(foreignCookie, "198.51.100.62")); recorder.Code != http.StatusUnauthorized {
			t.Fatalf("logged-out session = %d, want 401", recorder.Code)
		}
		// …and nobody else's session was touched.
		if recorder := harness.DoRequest(t, http.MethodGet, "/api/account", nil,
			withCookie(ownCookie, clientIP)); recorder.Code != http.StatusOK {
			t.Fatalf("the other buyer's session = %d, want it untouched (200)", recorder.Code)
		}
	})
}

// assertIsolationTableCoversEverySessionRoute fails when the router grows a route
// that nobody added to the isolation table, and when a route listed as public
// stops existing (a typo would otherwise hide a protected route).
func assertIsolationTableCoversEverySessionRoute(t *testing.T, engine *gin.Engine) {
	t.Helper()

	registered := make(map[string]bool)
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	expected := make(map[string]bool, len(publicRoutes))
	for _, route := range publicRoutes {
		expected[route] = true
		if !registered[route] {
			t.Fatalf("%q is listed as public but the router does not register it", route)
		}
	}

	// Every other route answers for one buyer, so it belongs in the table above.
	sessionRoutes := make([]string, 0, len(registered))
	for route := range registered {
		if expected[route] {
			continue
		}
		sessionRoutes = append(sessionRoutes, route)
	}
	sort.Strings(sessionRoutes)

	covered := map[string]bool{
		"GET /api/account":                true,
		"GET /api/account/extend-preview": true,
		"GET /api/auth/session":           true,
		"POST /api/auth/logout":           true,
		"GET /api/transactions":           true,
		"POST /api/transactions":          true,
		"GET /api/transactions/channels":  true,
		"GET /api/transactions/:id":       true,
	}
	for _, route := range sessionRoutes {
		if !covered[route] {
			t.Fatalf("route %q answers for one buyer but has no isolation case: "+
				"add it to the table in TestSessionEndpointsIsolatePhoneNumbers", route)
		}
	}
	deleteAll(covered, sessionRoutes)
	if len(covered) > 0 {
		remaining := make([]string, 0, len(covered))
		for route := range covered {
			remaining = append(remaining, route)
		}
		sort.Strings(remaining)
		t.Fatalf("the isolation table names routes the router no longer serves: %v", remaining)
	}
}

func deleteAll(covered map[string]bool, routes []string) {
	for _, route := range routes {
		delete(covered, route)
	}
}

// marshalBody encodes a request body, or returns nil for the bodyless cases.
func marshalBody(t *testing.T, payload map[string]any) []byte {
	t.Helper()

	if payload == nil {
		return nil
	}
	return mustJSON(t, payload)
}
