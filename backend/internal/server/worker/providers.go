package worker

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// TaskRegistrar is implemented by every domain worker package. Tasks are
// collected through the worker_tasks Fx group so the worker graph grows without
// editing a central switch.
type TaskRegistrar interface {
	RegisterHandlers(mux *asynq.ServeMux)
}

// NewServeMux builds the task mux and lets domains register their handlers.
func NewServeMux(registrars []TaskRegistrar) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	for _, registrar := range registrars {
		registrar.RegisterHandlers(mux)
	}
	return mux
}

// NewServer builds the Asynq server. It is the only process allowed to hold
// hardware (the SMS modem later on), which is why the API never starts one.
func NewServer(
	lc fx.Lifecycle,
	cfg *config.Config,
	client *redis.Client,
	mux *asynq.ServeMux,
	logger *zap.Logger,
) *asynq.Server {
	server := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:        cfg.Redis.Addr,
			Password:    cfg.Redis.Password,
			DB:          cfg.Redis.DB,
			DialTimeout: cfg.Redis.DialTimeout,
		},
		asynq.Config{
			Concurrency:     cfg.Worker.Concurrency,
			Queues:          queuePriorities(cfg.Worker.Queues),
			ShutdownTimeout: cfg.Server.ShutdownTimeout,
			Logger:          asynqLogger{logger: logger},
		},
	)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Asynq's Start performs no Redis I/O, so an unreachable Redis
			// would otherwise look like a healthy worker that never works.
			pingCtx, cancel := context.WithTimeout(ctx, cfg.Redis.DialTimeout)
			defer cancel()
			if err := client.Ping(pingCtx).Err(); err != nil {
				return fmt.Errorf("worker cannot reach redis: %w", err)
			}

			if err := server.Start(mux); err != nil {
				return fmt.Errorf("start worker: %w", err)
			}
			logger.Info("worker started",
				zap.Int("concurrency", cfg.Worker.Concurrency),
				zap.Strings("queues", cfg.Worker.Queues),
			)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("stopping worker")

			done := make(chan struct{})
			go func() {
				server.Shutdown()
				close(done)
			}()

			select {
			case <-done:
				logger.Info("worker stopped")
				return nil
			case <-ctx.Done():
				server.Stop()
				return fmt.Errorf("worker shutdown timed out: %w", ctx.Err())
			}
		},
	})

	return server
}

// queuePriorities maps the configured queue list to Asynq priorities: the first
// queue listed is served first.
func queuePriorities(queues []string) map[string]int {
	priorities := make(map[string]int, len(queues))
	for i, name := range queues {
		priorities[name] = i + 1
	}
	return priorities
}

// asynqLogger adapts Asynq's logger interface onto the injected zap logger.
type asynqLogger struct {
	logger *zap.Logger
}

func (l asynqLogger) Debug(args ...interface{}) { l.logger.Sugar().Debug(args...) }
func (l asynqLogger) Info(args ...interface{})  { l.logger.Sugar().Info(args...) }
func (l asynqLogger) Warn(args ...interface{})  { l.logger.Sugar().Warn(args...) }
func (l asynqLogger) Error(args ...interface{}) { l.logger.Sugar().Error(args...) }
func (l asynqLogger) Fatal(args ...interface{}) { l.logger.Sugar().Fatal(args...) }
