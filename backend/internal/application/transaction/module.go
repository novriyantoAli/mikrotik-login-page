// Package transaction is the Transaction slice: one Package purchase, its
// payment state and the instructions checkout renders. Provisioning (#15) starts
// from the `paid` state this domain produces.
package transaction

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// Module provides the repository and service for the API process. The HTTP
// handler is registered with the API transport, like every other domain.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			repository.NewRepository,
			func(cfg *config.Config) service.Settings {
				return service.Settings{
					PaymentTTL:         cfg.Transaction.PaymentTTL,
					LoginPageAllowlist: cfg.Auth.LoginPageAllowlist,
				}
			},
			service.NewService,
			// The API graph also offers the expiry surface so an integration test
			// can drive the deadline with the frozen clock instead of waiting.
			service.NewExpiry,
		),
	)
}

// WorkerModule provides only what the expiry sweep needs: the repository and the
// expiry surface, never a payment gateway or the catalog.
func WorkerModule() fx.Option {
	return fx.Options(
		fx.Provide(
			repository.NewRepository,
			service.NewExpiry,
		),
	)
}

// Migrations lists the Postgres tables this domain owns.
func Migrations() []any {
	return []any{&entity.Transaction{}}
}
