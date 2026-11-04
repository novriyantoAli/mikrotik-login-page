// Package admin is the Admin Panel's own identity, session and audit slice
// (ADR 0024, ADR 0027). It is separate from auth on purpose: the panel signs in
// with an email and a password so it still opens when the modem or the SMSC is
// the thing that broke, and its session table is never the buyer's.
package admin

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/handler"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
)

// Module provides the admin repository, the bootstrap/management service, the
// sign-in surface and the guard. The append-only audit adapter is provided by the
// composition root instead, because a process that builds one of its consumers
// (the worker builds the catalog) must be able to record without building this
// domain. The HTTP handler is registered with the API transport, like every other
// domain route.
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
			func(cfg *config.Config) service.AuthSettings {
				return service.AuthSettings{
					SessionTTL:  cfg.Admin.SessionTTL,
					IPWindow:    cfg.Admin.RateLimit.IPWindow,
					IPLimit:     cfg.Admin.RateLimit.IPLimit,
					EmailWindow: cfg.Admin.RateLimit.EmailWindow,
					EmailLimit:  cfg.Admin.RateLimit.EmailLimit,
				}
			},
			func(cfg *config.Config) middleware.AdminCookieSettings {
				return middleware.AdminCookieSettings{
					Name:   cfg.Admin.CookieName,
					Secure: cfg.Admin.CookieSecure,
					TTL:    cfg.Admin.SessionTTL,
				}
			},
			service.NewService,
			service.NewAuth,
			// Fx matches concrete interface types, so the narrow resolver the guard
			// depends on is bridged explicitly from the auth service.
			func(auth service.Auth) middleware.AdminSessionResolver { return auth },
			middleware.NewAdminGuard,
			handler.NewHandler,
			// The panel's catalog routes are a thin adapter over packages.Service; the
			// catalog's rules stay in the owning domain (golden rule 5).
			handler.NewPackageHandler,
		),
	)
}

// Migrations lists the Postgres tables this domain owns, for the deployment
// migration command (ADR 0012).
func Migrations() []any {
	return []any{&entity.Admin{}, &entity.Session{}, &entity.AuditEntry{}}
}
