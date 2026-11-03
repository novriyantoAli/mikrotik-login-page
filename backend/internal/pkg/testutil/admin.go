package testutil

import "testing"

// The first-owner pair every test schema is bootstrapped with. Neither is a
// secret: `migrate` refuses to finish on an empty admins table without a pair
// (ADR 0024 §6), so a test harness must hand it one, and a test that wants to
// prove the refusal overrides these to empty.
const (
	testAdminEmail = "owner@example.test"
	//nolint:gosec // a fixture for throwaway schemas, not a credential
	testAdminPassword = "kata-sandi-yang-cukup-panjang"
)

// AdminBootstrapEnv renders the ADMIN_EMAIL/ADMIN_PASSWORD overrides a test
// schema is seeded with.
func AdminBootstrapEnv() map[string]string {
	return map[string]string{
		"ADMIN_EMAIL":    testAdminEmail,
		"ADMIN_PASSWORD": testAdminPassword,
	}
}

// ApplyAdminBootstrapEnv installs the first-owner pair for the application under
// test.
func ApplyAdminBootstrapEnv(t *testing.T) {
	t.Helper()
	applyEnv(t, AdminBootstrapEnv())
}
