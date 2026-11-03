// Package admin is the Admin Panel's own identity, session and audit slice
// (ADR 0024, ADR 0027). It is separate from auth on purpose: the panel signs in
// with an email and a password so it still opens when the modem or the SMSC is
// the thing that broke, and its session table is never the buyer's.
package admin

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// Module provides the admin repository and the service. The append-only audit
// adapter is provided by the composition root instead, because a process that
// builds one of its consumers (the worker builds the catalog) must be able to
// record without building this domain. The HTTP handler is registered with the API
// transport, like every other domain route.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			repository.NewRepository,
			// Fx cannot inject two bare strings, so the bootstrap pair arrives as
			// the one settings struct the service reads.
			func(cfg *config.Config) service.BootstrapSettings {
				return service.BootstrapSettings{
					Email:    cfg.Admin.Email,
					Password: cfg.Admin.Password,
				}
			},
			service.NewService,
		),
	)
}

// Migrations lists the Postgres tables this domain owns, for the deployment
// migration command (ADR 0012).
func Migrations() []any {
	return []any{&entity.Admin{}, &entity.Session{}, &entity.AuditEntry{}}
}
