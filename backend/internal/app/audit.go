// This file is where the one audit implementation is bound to the one interface a
// recording domain depends on. The table and adapter belong to admin, while
// the interface lives in internal/pkg/audit with the vocabulary, so no domain
// imports admin and admin imports no domain (ADR 0027 §1).
package app

import (
	"go.uber.org/fx"

	adminrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
)

// The adapter satisfies the interface by construction; this assertion fails the
// build, not a request, if it drifts.
var _ audit.Recorder = (*adminrepository.AuditRecorder)(nil)

// bindAuditRecorder hands the adapter out under the interface domains depend on.
func bindAuditRecorder(recorder *adminrepository.AuditRecorder) audit.Recorder {
	return recorder
}

// auditBindings provides the append-only audit adapter as both the concrete type
// (the admin repository records its own writes) and the interface every other
// domain asks for.
//
// It is part of every composition root on purpose: a process that builds a
// consumer must be able to record without building the admin domain. The worker
// no longer consumes the catalog, but the binding stays because the graph, not a
// process list, decides who may record.
func auditBindings() fx.Option {
	return fx.Options(
		fx.Provide(
			adminrepository.NewAuditRecorder,
			bindAuditRecorder,
		),
	)
}
