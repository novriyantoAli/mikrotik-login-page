package audit_test

import (
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// TestActionNamesAreStable is the whole point of the vocabulary: a report written
// against today's rows must still match rows written next year (ADR 0027 §3). A
// rename here is a migration, not a refactor.
func TestActionNamesAreStable(t *testing.T) {
	want := map[audit.Action]string{
		audit.ActionPackageCreate:      "package.create",
		audit.ActionPackageUpdate:      "package.update",
		audit.ActionPackageActivate:    "package.activate",
		audit.ActionPackageDeactivate:  "package.deactivate",
		audit.ActionPINReveal:          "pin.reveal",
		audit.ActionAdminCreate:        "admin.create",
		audit.ActionAdminUpdate:        "admin.update",
		audit.ActionAdminPasswordReset: "admin.password_reset",
		audit.ActionAdminDeactivate:    "admin.deactivate",
	}

	for action, name := range want {
		if string(action) != name {
			t.Fatalf("action %q is spelled %q, want %q", action, string(action), name)
		}
	}

	targets := map[audit.TargetType]string{
		audit.TargetPackage:     "package",
		audit.TargetTransaction: "transaction",
		audit.TargetAdmin:       "admin",
	}
	for target, name := range targets {
		if string(target) != name {
			t.Fatalf("target type %q is spelled %q, want %q", target, string(target), name)
		}
	}
}

// TestWithTargetReturnsACopy keeps the completed fact from rewriting the one the
// service built: a repository learns the new row's id, not the action's intent.
func TestWithTargetReturnsACopy(t *testing.T) {
	original := audit.Entry{Action: audit.ActionAdminCreate, Payload: map[string]any{"role": "support"}}

	completed := original.WithTarget(audit.TargetAdmin, "9")

	if completed.TargetType != audit.TargetAdmin || completed.TargetID != "9" {
		t.Fatalf("completed target = %q/%q, want admin/9", completed.TargetType, completed.TargetID)
	}
	if original.TargetType != "" || original.TargetID != "" {
		t.Fatalf("the original entry was mutated: %q/%q", original.TargetType, original.TargetID)
	}
}
