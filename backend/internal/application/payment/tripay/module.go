package tripay

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// Module provides the Tripay adapter as the payment.Gateway port. The wiring
// lives in this package, not in payment, because the adapter imports the port:
// a module.go next to the port would close an import cycle.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			func(cfg *config.Config) Settings {
				return Settings{
					APIKey:       cfg.Tripay.APIKey,
					PrivateKey:   cfg.Tripay.PrivateKey,
					MerchantCode: cfg.Tripay.MerchantCode,
					BaseURL:      cfg.Tripay.BaseURL,
					CallbackURL:  cfg.Tripay.CallbackURL,
					ReturnURL:    cfg.Tripay.ReturnURL,
					Timeout:      cfg.Tripay.RequestTimeout,
				}
			},
			New,
			func(adapter *Adapter) payment.Gateway { return adapter },
		),
	)
}
