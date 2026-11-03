// The audit port is declared here, in the domain that records the action, not in
// the admin domain that stores it: the catalog then never imports admin, and
// admin never imports the catalog (ADR 0027 §1). The adapter is the admin
// domain's AuditRecorder, bound to this interface by the composition root.
package repository

import (
	"context"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// AuditRecorder appends the one trail row a privileged catalog write must leave.
//
// It takes the caller's transaction rather than opening its own, so the catalog
// change and its audit row commit or roll back together and "price changed with
// no witness" is not representable (ADR 0027 §2). The panel's write path is the
// first caller (#32).
type AuditRecorder interface {
	Record(ctx context.Context, tx database.Tx, entry audit.Entry) error
}
