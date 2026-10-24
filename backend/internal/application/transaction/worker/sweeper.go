package worker

import (
	"context"
	"sync"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

// sweeper runs one idempotent background job on a ticker and whenever its
// wake-up task arrives. Expiry and provisioning both need exactly this, so the
// lifecycle handling — the part that is easier to get wrong than the job — exists
// once.
type sweeper struct {
	every  time.Duration
	task   func(context.Context)
	logger *zap.Logger

	wake chan struct{}
	done sync.WaitGroup
}

func newSweeper(every time.Duration, task func(context.Context), logger *zap.Logger) *sweeper {
	return &sweeper{
		every:  every,
		task:   task,
		logger: logger,
		wake:   make(chan struct{}, 1),
	}
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
		s.task(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}
