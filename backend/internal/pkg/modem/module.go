package modem

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// Module provides the modem as the SMS port. It is wired into the worker
// process only: the API server must never open the device (ADR 0010).
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			func(cfg *config.Config) Settings {
				return Settings{
					Device:         cfg.SMS.Device,
					Baud:           cfg.SMS.Baud,
					CommandTimeout: cfg.SMS.CommandTimeout,
					SubmitTimeout:  cfg.SMS.SubmitTimeout,
				}
			},
			fx.Annotate(New, fx.As(new(sms.Sender))),
		),
	)
}
