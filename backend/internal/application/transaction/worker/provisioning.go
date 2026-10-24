package worker

import (
	"context"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/sweep"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
)

// Provisioning runs the account work in the worker process, which is the only
// process holding the FreeRADIUS connection and the modem (ADR 0016). A paid
// Transaction that settlement left behind is picked up by the sweep, so a lost
// wake-up costs one interval instead of the buyer's account.
type Provisioning struct {
	sweeper *sweeper
}

// NewProvisioning starts the provisioning sweep with the process lifecycle.
func NewProvisioning(
	lc fx.Lifecycle,
	provisioner service.Provisioner,
	settings service.ProvisioningSettings,
	registry *sweep.Registry,
	logger *zap.Logger,
) *Provisioning {
	sweeper := newSweeper(settings.SweepInterval, service.TaskProvision, registry, func(ctx context.Context) {
		done, err := provisioner.ProvisionDue(ctx)
		if err != nil {
			logger.Error("provision paid transactions", zap.Error(err))
			return
		}
		if done > 0 {
			logger.Info("paid transactions provisioned", zap.Int("count", done))
		}
	}, logger)
	sweeper.attach(lc)

	return &Provisioning{sweeper: sweeper}
}

// RegisterHandlers implements the worker's TaskRegistrar.
func (p *Provisioning) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(service.TaskProvision, func(context.Context, *asynq.Task) error {
		p.sweeper.wakeUp()
		return nil
	})
}
