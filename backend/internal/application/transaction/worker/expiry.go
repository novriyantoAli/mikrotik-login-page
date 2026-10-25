// Package worker runs the Transaction background halves in the worker process: it
// closes overdue payments and provisions paid accounts, so no request waits for a
// clock tick or for MySQL (PRD points 20 and 22).
package worker

import (
	"context"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/sweep"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
)

// TaskExpire is the wake-up task. It carries no work: the rule is "expire every
// pending Transaction whose deadline passed", which is safe to run any number of
// times.
const TaskExpire = "transaction:expire"

// Expiry sweeps the Transaction table on an interval and on demand.
type Expiry struct {
	sweeper *sweeper
}

// NewExpiry starts the sweeper with the process lifecycle.
func NewExpiry(
	lc fx.Lifecycle,
	expiry service.Expiry,
	settings service.SweepSettings,
	registry *sweep.Registry,
	clk clock.Clock,
	logger *zap.Logger,
) *Expiry {
	sweeper := newSweeper(settings.Expire.Interval, TaskExpire, registry, clk, func(ctx context.Context) {
		if _, err := expiry.ExpireDue(ctx); err != nil {
			logger.Error("expire pending transactions", zap.Error(err))
		}
	}, logger)
	sweeper.attach(lc)

	return &Expiry{sweeper: sweeper}
}

// RegisterHandlers implements the worker's TaskRegistrar.
func (e *Expiry) RegisterHandlers(mux *asynq.ServeMux) {
	e.sweeper.register(mux, TaskExpire)
}
