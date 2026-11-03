// This file is where one domain's implementation is bound to another domain's
// port. The audit trail has exactly that shape: the table and the adapter belong
// to admin, while the ports belong to the domains that record an action
// (ADR 0027 §1). Binding them here keeps both directions clean — the catalog never
// imports admin, and admin never imports the catalog.
package app

import (
	"go.uber.org/fx"

	adminrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	packagesrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	transactionrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
)

// The adapter satisfies every port by construction; these assertions fail the
// build, not a request, if one drifts.
var (
	_ packagesrepository.AuditRecorder    = (*adminrepository.AuditRecorder)(nil)
	_ transactionrepository.AuditRecorder = (*adminrepository.AuditRecorder)(nil)
)

// auditBindings provides the append-only audit adapter and binds it to each
// domain's own port.
//
// It is part of every composition root on purpose: a process that builds a
// consumer must be able to record, and the worker builds the catalog without
// building the admin domain. Adding a consumer is therefore a change to that
// domain, never a second wiring edit here.
func auditBindings() fx.Option {
	return fx.Options(
		fx.Provide(
			adminrepository.NewAuditRecorder,
			recordAudit,
		),
	)
}

// recordAudit hands the one concrete recorder out under the interfaces the
// domains declared for themselves.
func recordAudit(recorder *adminrepository.AuditRecorder) (
	packagesrepository.AuditRecorder,
	transactionrepository.AuditRecorder,
) {
	return recorder, recorder
}
