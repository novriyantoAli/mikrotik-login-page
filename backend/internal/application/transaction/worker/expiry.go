// Package worker runs the Transaction deadline in the background process. It
// shares the drainer's shape: an interval sweep for the normal case, plus a
// wake-up task so an operator can nudge it without waiting for the tick.
package worker

import (
	"context"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
)

// TaskExpire is the wake-up task. It carries no work: the rule is "expire every
// pending Transaction whose deadline passed", which is safe to run any number of
// times.
const TaskExpire = "transaction:expire"

// Settings are the sweeper knobs, kept in a local struct because Fx cannot
// resolve a bare time.Duration.
type Settings struct {
	// SweepInterval is how often overdue pending Transactions are closed.
	SweepInterval time.Duration
}

// Expiry sweeps the Transaction table on an interval and on demand.
type Expiry struct {
	expiry service.Expiry
	logger *zap.Logger
	sweep  time.Duration

	wake chan struct{}
	done sync.WaitGroup
}

// NewExpiry starts the sweeper with the process lifecycle.
func NewExpiry(lc fx.Lifecycle, expiry service.Expiry, settings Settings, logger *zap.Logger) *Expiry {
	sweeper := &Expiry{
		expiry: expiry,
		logger: logger,
		sweep:  settings.SweepInterval,
		wake:   make(chan struct{}, 1),
	}

	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			// Fx's start context is canceled once startup finishes, so the sweep
			// gets its own lifetime tied to OnStop.
			//nolint:gosec // canceled from the OnStop hook below, not on this path
			ctx, stop := context.WithCancel(context.Background())
			cancel = stop

			sweeper.done.Add(1)
			go sweeper.run(ctx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}

			stopped := make(chan struct{})
			go func() {
				sweeper.done.Wait()
				close(stopped)
			}()

			select {
			case <-stopped:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})

	return sweeper
}

// RegisterHandlers implements the worker's TaskRegistrar.
func (e *Expiry) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TaskExpire, func(_ context.Context, _ *asynq.Task) error {
		e.wakeUp()
		return nil
	})
}

func (e *Expiry) wakeUp() {
	select {
	case e.wake <- struct{}{}:
	default:
		// A sweep is already pending; one is enough.
	}
}

func (e *Expiry) run(ctx context.Context) {
	defer e.done.Done()

	ticker := time.NewTicker(e.sweep)
	defer ticker.Stop()

	for {
		e.expire(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-e.wake:
		}
	}
}

func (e *Expiry) expire(ctx context.Context) {
	if _, err := e.expiry.ExpireDue(ctx); err != nil {
		e.logger.Error("expire pending transactions", zap.Error(err))
	}
}
