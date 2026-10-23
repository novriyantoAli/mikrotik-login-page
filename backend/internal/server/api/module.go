// Package api wires the HTTP transport: engine, middleware, route registration
// and the listener. RouterModule is separate from ListenerModule so integration
// tests can exercise the exact production router without binding a port.
package api

import (
	"net/http"

	"go.uber.org/fx"

	activeperiodhandler "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod/handler"
	authhandler "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth/handler"
	healthhandler "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/handler"
	packagehandler "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/handler"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/middleware"
)

const routesGroup = `group:"api_routes"`

// RouterModule provides the engine and every domain route.
func RouterModule() fx.Option {
	return fx.Options(
		fx.Provide(
			NewEngine,
			// The session guard is transport-level: HTTP routes that need a
			// buyer's identity depend on it, and it authorises through the auth
			// service rather than reading the sessions table (PRD point 13).
			func(cfg *config.Config) middleware.SessionSettings {
				return middleware.SessionSettings{
					CookieName: cfg.Auth.CookieName,
					Secure:     cfg.Auth.CookieSecure,
				}
			},
			middleware.NewSessionGuard,
			// RouteRegistrar is the second parameter; the engine itself is not
			// part of the group.
			fx.Annotate(NewRoutingEngine, fx.ParamTags("", routesGroup)),
			fx.Annotate(
				healthhandler.NewHandler,
				fx.As(new(RouteRegistrar)),
				fx.ResultTags(routesGroup),
			),
			fx.Annotate(
				packagehandler.NewHandler,
				fx.As(new(RouteRegistrar)),
				fx.ResultTags(routesGroup),
			),
			fx.Annotate(
				activeperiodhandler.NewHandler,
				fx.As(new(RouteRegistrar)),
				fx.ResultTags(routesGroup),
			),
			fx.Annotate(
				authhandler.NewHandler,
				fx.As(new(RouteRegistrar)),
				fx.ResultTags(routesGroup),
			),
		),
	)
}

// ListenerModule binds the router to the configured address. The invoke is what
// makes Fx construct the server: its lifecycle hooks are appended in the
// constructor, so without it the provider would never run.
func ListenerModule() fx.Option {
	return fx.Options(
		fx.Provide(NewHTTPServer),
		fx.Invoke(func(*http.Server) {}),
	)
}

// Module is the complete HTTP transport.
func Module() fx.Option {
	return fx.Options(RouterModule(), ListenerModule())
}
