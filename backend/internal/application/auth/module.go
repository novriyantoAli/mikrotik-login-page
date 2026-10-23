// Package auth is the passwordless entry slice: Magic Link tokens, opaque
// sessions and the rate limits around them (ADR 0011). It owns two Postgres
// tables and no HTTP-specific behavior beyond the thin handler.
package auth

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/handler"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// Module provides the auth repository, service, settings and handler. The route
// registrar annotation lives in the HTTP transport, like every other domain.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			repository.NewRepository,
			func(cfg *config.Config) service.Settings {
				return service.Settings{
					MagicLinkTTL:       cfg.Auth.MagicLinkTTL,
					SessionTTL:         cfg.Auth.SessionTTL,
					AppBaseURL:         cfg.Auth.AppBaseURL,
					MagicLinkPath:      cfg.Auth.MagicLinkPath,
					LoginPageAllowlist: cfg.Auth.LoginPageAllowlist,
					PhoneWindow:        cfg.Auth.RateLimit.PhoneWindow,
					PhoneLimit:         cfg.Auth.RateLimit.PhoneLimit,
					IPWindow:           cfg.Auth.RateLimit.IPWindow,
					IPLimit:            cfg.Auth.RateLimit.IPLimit,
				}
			},
			func(cfg *config.Config) handler.CookieSettings {
				return handler.CookieSettings{
					Name:   cfg.Auth.CookieName,
					Secure: cfg.Auth.CookieSecure,
					TTL:    cfg.Auth.SessionTTL,
				}
			},
			service.NewService,
			handler.NewHandler,
		),
	)
}

// Migrations lists the Postgres tables this domain owns.
func Migrations() []any {
	return []any{&entity.MagicLinkToken{}, &entity.Session{}}
}
