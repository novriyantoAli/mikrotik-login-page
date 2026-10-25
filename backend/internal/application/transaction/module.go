// Package transaction is the Transaction slice: one Package purchase, its
// payment state and the instructions checkout renders. Provisioning turns the
// `paid` state this domain produces into a working hotspot account (ADR 0016).
package transaction

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/pin"
)

// Module provides the repository and the service for the API process. The HTTP
// handler is registered with the API transport, like every other domain. The
// expiry, provisioning and reconciliation surfaces are offered here too, so an
// integration test can drive them through the production graph instead of
// waiting for a tick.
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
			sweepSettings,
			newPINCipher,
			service.NewService,
			service.NewExpiry,
			service.NewProvisioner,
			service.NewReconciler,
		),
	)
}

// WorkerModule provides what the background process needs: the deadline sweep,
// provisioning and the lost-callback reconciliation. It never builds the
// catalog handler or the HTTP transport.
func WorkerModule() fx.Option {
	return fx.Options(
		fx.Provide(
			repository.NewRepository,
			sweepSettings,
			newPINCipher,
			service.NewExpiry,
			service.NewProvisioner,
			service.NewReconciler,
		),
	)
}

// Migrations lists the Postgres tables this domain owns.
func Migrations() []any {
	return []any{&entity.Transaction{}}
}

// sweepSettings is the one place the three worker cadences are read from config:
// they are one concept, so the domain receives them as one value.
func sweepSettings(cfg *config.Config) service.SweepSettings {
	return service.SweepSettings{
		Expire:    service.SweepCadence{Interval: cfg.Transaction.ExpirySweepInterval},
		Provision: service.SweepCadence{Interval: cfg.PIN.SweepInterval},
		Reconcile: service.SweepCadence{
			Interval: cfg.Transaction.ReconcileSweepInterval,
			Batch:    cfg.Transaction.ReconcileBatch,
		},
	}
}

// newPINCipher is the one place the deployment's key material is parsed; every
// process that reads a PIN needs it, so it is a provider rather than a config
// field (ADR 0016).
func newPINCipher(cfg *config.Config) (*pin.Cipher, error) {
	return pin.NewCipher(pin.Settings{
		Keys:          cfg.PIN.EncryptionKeys,
		ActiveVersion: cfg.PIN.ActiveKeyVersion,
	})
}
