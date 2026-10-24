// Package account is the Account Status read composition. It owns no entity and
// no repository by design: PRD point 6 says Phone Number is the only identity and
// there is no account table, so everything the page shows is read from the domains
// that already own it (ADR 0017). There is no worker module: the page is read-only
// and only the API process serves it.
package account

import (
	"time"

	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// Module provides the Account Status service. The HTTP handler is registered
// with the API transport, like every other domain route.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			func(cfg *config.Config) service.Settings {
				return service.Settings{
					ExpiryWarning: time.Duration(cfg.Account.ExpiryWarningHours) * time.Hour,
				}
			},
			service.NewService,
		),
	)
}
