// The audit port is declared here, in the domain that records the action, not in
// the admin domain that stores it: Transaction then never imports admin, and
// admin never imports Transaction (ADR 0027 §1). The adapter is the admin
// domain's AuditRecorder, bound to this interface by the composition root.
package repository

import (
	"context"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// AuditRecorder appends the one trail row a privileged Transaction action must
// leave.
//
// It takes the caller's transaction rather than opening its own, so the change and
// its audit row commit or roll back together (ADR 0027 §2). A PIN reveal is
// audited whether or not it succeeds — the attempt is the interesting fact
// (ADR 0027 §4) — which is why the port is usable before the decrypt is known to
// have worked. The reveal path is the first caller (#36).
type AuditRecorder interface {
	Record(ctx context.Context, tx database.Tx, entry audit.Entry) error
}
