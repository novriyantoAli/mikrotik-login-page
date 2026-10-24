// Package notification is the SMS slice: one outbox table, an enqueue path the
// other domains use, and a worker that owns the modem (ADR 0010). The modem
// itself is a vendor adapter behind the Sender port in port.go.
package notification

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/queue"
)

// Module provides the enqueue path: other domains depend on service.Outbox and
// never see a modem. The Asynq wake-up client comes with it.
func Module() fx.Option {
	return fx.Options(
		queue.Module(),
		fx.Provide(
			repository.NewRepository,
			service.NewOutbox,
		),
	)
}

// WorkerModule provides both worker surfaces: the delivery path the drainer
// consumes, and the enqueue path, because provisioning records its PIN SMS in the
// same transaction and the same process (ADR 0016). The drainer that consumes it
// is registered by the worker process, the only one allowed to hold the device
// (ADR 0010).
func WorkerModule() fx.Option {
	return fx.Options(
		queue.Module(),
		fx.Provide(
			repository.NewRepository,
			service.NewOutbox,
			func(cfg *config.Config) service.Settings {
				return service.Settings{
					Lease:        cfg.SMS.Lease,
					MaxAttempts:  cfg.SMS.MaxAttempts,
					RetryBackoff: cfg.SMS.RetryBackoff,
				}
			},
			service.NewDeliverer,
		),
	)
}

// Migrations lists the Postgres tables this domain owns.
func Migrations() []any {
	return []any{&entity.SMS{}}
}
