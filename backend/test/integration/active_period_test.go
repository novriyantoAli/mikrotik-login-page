package integration

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// TestActivePeriodAcceptsEveryPhoneNumberForm is the issue's acceptance
// criterion: 08…, +62… and spaced input all resolve to the same account.
func TestActivePeriodAcceptsEveryPhoneNumberForm(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	expiresAt := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	harness.Doubles.RadiusReader.SetExpiration("628123456789", expiresAt)

	for _, input := range []string{
		"08123456789",
		"0812-3456-789",
		"0812 3456 789",
		"+628123456789",
		"+62 812 3456 789",
		"62 812 3456 789",
		"628123456789",
	} {
		status, body := harness.GetJSON(t, "/api/active-period?phone="+url.QueryEscape(input))
		if status != 200 {
			t.Fatalf("%q: status = %d, body = %v", input, status, body)
		}

		data, ok := body["data"].(map[string]any)
		if !ok {
			t.Fatalf("%q: data is not an object: %T", input, body["data"])
		}
		if data["expiry"] != "2026-01-10T12:00:00Z" {
			t.Fatalf("%q: expiry = %v, want the ISO-8601 instant in UTC", input, data["expiry"])
		}
	}
}

// TestActivePeriodUnknownNumberIsAnEmptyState covers the issue's "no 5xx for an
// unknown number": a valid Phone Number without an expiration is an empty
// Active Period, not an error.
func TestActivePeriodUnknownNumberIsAnEmptyState(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	status, body := harness.GetJSON(t, "/api/active-period?phone=628999999999")
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}

	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data is not an object: %T", body["data"])
	}
	expiry, present := data["expiry"]
	if !present {
		t.Fatalf("expiry field is missing: %v", data)
	}
	if expiry != nil {
		t.Fatalf("expiry = %v, want null", expiry)
	}
}

// TestActivePeriodRejectsMalformedNumbers keeps a typo distinguishable from an
// account without an Active Period.
func TestActivePeriodRejectsMalformedNumbers(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	for _, input := range []string{"", "not-a-number", "0812", "0211234567"} {
		status, body := harness.GetJSON(t, "/api/active-period?phone="+url.QueryEscape(input))
		if status != 400 {
			t.Fatalf("%q: status = %d, want 400", input, status)
		}
		errorBody, ok := body["error"].(map[string]any)
		if !ok {
			t.Fatalf("%q: response has no error envelope: %v", input, body)
		}
		if errorBody["code"] != "invalid_request" {
			t.Fatalf("%q: error code = %v, want invalid_request", input, errorBody["code"])
		}
	}
}

// TestActivePeriodUnreachableFreeRADIUSIsAServiceUnavailable isolates the vendor
// failure: the adapter, not a fake, is asked to read a closed MySQL port.
func TestActivePeriodUnreachableFreeRADIUSIsAServiceUnavailable(t *testing.T) {
	harness := testutil.NewAPIHarness(t,
		testutil.WithFreeRADIUSMySQLDSN(""),
		testutil.WithRealRadiusReader(),
	)

	status, body := harness.GetJSON(t, "/api/active-period?phone=628123456789")
	if status != 503 {
		t.Fatalf("status = %d, want 503", status)
	}

	errorBody, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("response has no error envelope: %v", body)
	}
	if errorBody["code"] != "dependency_unavailable" {
		t.Fatalf("error code = %v, want dependency_unavailable", errorBody["code"])
	}
}

// TestActivePeriodReaderFailureIsNotAnEmptyState proves a failing read is never
// rendered as "this account has no Active Period".
func TestActivePeriodReaderFailureIsNotAnEmptyState(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	harness.Doubles.RadiusReader.Err = errors.New("mysql is down")

	status, body := harness.GetJSON(t, "/api/active-period?phone=628123456789")
	if status != 503 {
		t.Fatalf("status = %d, want 503", status)
	}
	if _, isData := body["data"]; isData {
		t.Fatalf("failure returned a data envelope: %v", body)
	}
}
