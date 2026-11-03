package integration

import (
	"testing"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"

	fxapp "github.com/novriyantoAli/mikrotik-login-page/backend/internal/app"
	packagesrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/repository"
	transactionrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	servermigration "github.com/novriyantoAli/mikrotik-login-page/backend/internal/server/migration"
)

// TestWorkerGraphResolves is the only test that builds the background process's
// composition root. Every HTTP test drives the API graph, so a provider the
// worker forgets (the gateway reconciliation needs, say) would otherwise pass
// the whole suite and fail only when the container starts (ADR 0018).
//
// fx.ValidateApp walks the graph without running constructors or lifecycle hooks,
// so no Postgres, Redis or modem is touched; the invoke names the three sweeps
// the worker exists to run, because a dry run only checks what something asks
// for. The audit ports are named too: the worker builds the catalog domain without
// building the admin domain, so a port the composition root forgot would only
// surface when the container starts (ADR 0027 §1).
func TestWorkerGraphResolves(t *testing.T) {
	t.Parallel()

	err := fx.ValidateApp(
		fxapp.Worker(),
		fx.Invoke(func(
			_ *asynq.ServeMux,
			_ transactionservice.Expiry,
			_ transactionservice.Provisioner,
			_ transactionservice.Reconciler,
			_ packagesrepository.AuditRecorder,
			_ transactionrepository.AuditRecorder,
		) {
		}),
	)
	if err != nil {
		t.Fatalf("worker graph does not resolve: %v", err)
	}
}

// TestMigrateGraphResolves covers the other composition root no HTTP test builds:
// the one-shot migration command.
func TestMigrateGraphResolves(t *testing.T) {
	t.Parallel()

	err := fx.ValidateApp(
		fxapp.Migrate(),
		fx.Invoke(func(*servermigration.Migrator) {}),
	)
	if err != nil {
		t.Fatalf("migrate graph does not resolve: %v", err)
	}
}
