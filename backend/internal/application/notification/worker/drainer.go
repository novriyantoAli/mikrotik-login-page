// Package worker delivers queued SMS messages. It is the only place a modem is
// touched, it runs in the worker process only, and every dialog is serialized
// (ADR 0010).
package worker

import (
	"context"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
)

// batchSize bounds one drain pass. The drainer stops early on the first modem
// failure, so the batch size only caps a healthy run.
const batchSize = 20

// Settings are the drainer knobs, kept in a local struct because Fx cannot
// resolve a bare time.Duration.
type Settings struct {
	// SweepInterval is how often the outbox is checked even without a wake-up.
	SweepInterval time.Duration
}

// Drainer owns the modem. It sweeps the outbox on an interval and also wakes up
// immediately when a domain enqueues the sms:deliver task, so a fresh Magic Link
// does not wait for the next tick.
type Drainer struct {
	deliverer service.Deliverer
	logger    *zap.Logger
	sweep     time.Duration

	wake chan struct{}
	done sync.WaitGroup
}

// NewDrainer starts the drainer with the process lifecycle.
func NewDrainer(lc fx.Lifecycle, deliverer service.Deliverer, settings Settings, logger *zap.Logger) *Drainer {
	drainer := &Drainer{
		deliverer: deliverer,
		logger:    logger,
		sweep:     settings.SweepInterval,
		wake:      make(chan struct{}, 1),
	}

	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			// Fx's start context is canceled as soon as startup finishes, so the
			// drainer gets its own lifetime tied to OnStop.
			//nolint:gosec // canceled from the OnStop hook below, not on this path
			ctx, stop := context.WithCancel(context.Background())
			cancel = stop

			drainer.done.Add(1)
			go drainer.run(ctx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}

			stopped := make(chan struct{})
			go func() {
				drainer.done.Wait()
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

	return drainer
}

// RegisterHandlers implements the worker's TaskRegistrar: the wake-up task only
// nudges the loop, it never carries work of its own.
func (d *Drainer) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(sms.TaskDeliver, d.handleWakeUp)
}

func (d *Drainer) handleWakeUp(_ context.Context, _ *asynq.Task) error {
	d.wakeUp()
	return nil
}

func (d *Drainer) wakeUp() {
	select {
	case d.wake <- struct{}{}:
	default:
		// A wake-up is already pending; one is enough.
	}
}

func (d *Drainer) run(ctx context.Context) {
	defer d.done.Done()

	ticker := time.NewTicker(d.sweep)
	defer ticker.Stop()

	for {
		d.drain(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.wake:
		}
	}
}

func (d *Drainer) drain(ctx context.Context) {
	delivered, err := d.deliverer.DeliverDue(ctx, batchSize)
	if err != nil {
		// The outbox row is untouched by a claim error, so the next sweep retries.
		if ctx.Err() == nil {
			d.logger.Error("drain sms outbox", zap.Error(err))
		}
		return
	}
	if delivered > 0 {
		d.logger.Info("sms batch delivered", zap.Int("messages", delivered))
	}
}
