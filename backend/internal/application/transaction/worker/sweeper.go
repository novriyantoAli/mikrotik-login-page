package worker

import (
	"context"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/sweep"
)

// sweeper runs one idempotent background job on a ticker and whenever its
// wake-up task arrives. Expiry, provisioning and reconciliation all need exactly
// this, so the lifecycle handling — the part that is easier to get wrong than the
// job — exists once. Every completed pass beats the sweep registry, which is how
// the worker can report "still working" instead of only "still running"
// (ADR 0018).
type sweeper struct {
	every    time.Duration
	name     string
	registry *sweep.Registry
	clock    clock.Clock
	task     func(context.Context)
	logger   *zap.Logger

	wake chan struct{}
	done sync.WaitGroup
}

func newSweeper(
	every time.Duration,
	name string,
	registry *sweep.Registry,
	clk clock.Clock,
	task func(context.Context),
	logger *zap.Logger,
) *sweeper {
	return &sweeper{
		every:    every,
		name:     name,
		registry: registry,
		clock:    clk,
		task:     task,
		logger:   logger,
		wake:     make(chan struct{}, 1),
	}
}

// register wires a payload-free wake-up task: the handler only nudges the loop,
// the durable work item is the data the sweep reads. All three sweeps would
// otherwise carry the same four lines (ADR 0018).
func (s *sweeper) register(mux *asynq.ServeMux, taskType string) {
	mux.HandleFunc(taskType, func(context.Context, *asynq.Task) error {
		s.wakeUp()
		return nil
	})
}

// attach starts the sweep with the process lifecycle and stops it on shutdown.
func (s *sweeper) attach(lc fx.Lifecycle) {
	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			// Fx's start context is canceled once startup finishes, so the sweep gets
			// its own lifetime tied to OnStop.
			//nolint:gosec // canceled from the OnStop hook below, not on this path
			ctx, stop := context.WithCancel(context.Background())
			cancel = stop

			s.done.Add(1)
			go s.run(ctx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}

			stopped := make(chan struct{})
			go func() {
				s.done.Wait()
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
}

// pass runs the job once and records the heartbeat. It is a method of its own so a
// test can drive one pass without starting the loop, and so the clock used for the
// beat is the one the checker reads.
func (s *sweeper) pass(ctx context.Context) {
	s.task(ctx)
	s.registry.Beat(s.name, s.every, s.clock.Now())
}

// wakeUp asks for an immediate sweep; one pending request is enough.
func (s *sweeper) wakeUp() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *sweeper) run(ctx context.Context) {
	defer s.done.Done()

	ticker := time.NewTicker(s.every)
	defer ticker.Stop()

	for {
		s.pass(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}
