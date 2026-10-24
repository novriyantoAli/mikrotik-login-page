package worker

import (
	"context"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
)

// TaskReconcile is the wake-up task for the reconciliation sweep. It carries no
// work: the rule is "ask the gateway about every pending Transaction still
// payable", which is safe to run any number of times (ADR 0018).
const TaskReconcile = "transaction:reconcile"

// Reconciliation asks the payment gateway about pending Transactions whose
// callback never arrived, so a paid buyer reaches `paid` without one. It enters
// through the same settle path a callback uses, so provisioning cannot happen
// twice (ADR 0018).
type Reconciliation struct {
	sweeper *sweeper
}

// NewReconciliation starts the reconciliation sweep with the process lifecycle.
func NewReconciliation(
	lc fx.Lifecycle,
	reconciler service.Reconciler,
	settings service.ReconciliationSettings,
	logger *zap.Logger,
) *Reconciliation {
	sweeper := newSweeper(settings.SweepInterval, func(ctx context.Context) {
		reconciled, err := reconciler.ReconcileDue(ctx)
		if err != nil {
			logger.Error("reconcile pending transactions", zap.Error(err))
			return
		}
		if reconciled > 0 {
			logger.Info("pending transactions reconciled", zap.Int("count", reconciled))
		}
	}, logger)
	sweeper.attach(lc)

	return &Reconciliation{sweeper: sweeper}
}

// RegisterHandlers implements the worker's TaskRegistrar.
func (r *Reconciliation) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TaskReconcile, func(context.Context, *asynq.Task) error {
		r.sweeper.wakeUp()
		return nil
	})
}
