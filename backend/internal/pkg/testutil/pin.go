package testutil

import "testing"

// testPINKey is a fixed 32-byte AES key every test process loads. It is not a
// secret: the deployed stack reads real key material from the environment, and
// only the versioned format matters to the tests (ADR 0016).
const testPINKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// PINEnv renders the PIN encryption overrides, so a test never depends on a
// developer's local configuration.
func PINEnv() map[string]string {
	return map[string]string{
		"PIN_ENCRYPTION_KEYS":    "1=" + testPINKey,
		"PIN_ACTIVE_KEY_VERSION": "1",
	}
}

// ApplyPINEnv installs the test PIN keys for the application under test.
func ApplyPINEnv(t *testing.T) {
	t.Helper()
	applyEnv(t, PINEnv())
}
