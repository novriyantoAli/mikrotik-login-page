// Package queue is the thin Asynq edge: it lets a domain ask the worker for
// immediate attention without knowing anything about Redis. Asynq tasks are
// wake-ups only; the durable record of any background work lives in Postgres
// (ADR 0010), so a lost task is never lost work.
package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// Enqueuer is the surface domains depend on.
type Enqueuer interface {
	// Enqueue asks the worker to run taskType soon. Identical wake-ups are
	// collapsed inside a short window, because the drainer sweeps anyway.
	Enqueue(ctx context.Context, taskType string) error
}

// uniqueWindow collapses repeated wake-ups for the same task type.
const uniqueWindow = 5 * time.Second

type client struct {
	asynq  *asynq.Client
	logger *zap.Logger
}

// NewClient builds the Asynq client and closes it with the process.
func NewClient(lc fx.Lifecycle, cfg *config.Config, logger *zap.Logger) *client {
	asynqClient := asynq.NewClient(asynq.RedisClientOpt{
		Addr:        cfg.Redis.Addr,
		Password:    cfg.Redis.Password,
		DB:          cfg.Redis.DB,
		DialTimeout: cfg.Redis.DialTimeout,
	})

	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			return asynqClient.Close()
		},
	})

	return &client{asynq: asynqClient, logger: logger}
}

// Enqueue implements Enqueuer. It never fails a caller's request when Redis is
// unreachable: the worker also sweeps for due rows, so a missed wake-up only
// delays delivery (ADR 0010).
func (c *client) Enqueue(ctx context.Context, taskType string) error {
	task := asynq.NewTask(taskType, nil)

	_, err := c.asynq.EnqueueContext(ctx, task,
		asynq.Queue("critical"),
		asynq.Unique(uniqueWindow),
		asynq.MaxRetry(0),
	)
	if err != nil {
		c.logger.Warn("enqueue wake-up task",
			zap.String("task", taskType),
			zap.Error(err),
		)
		return fmt.Errorf("enqueue %s: %w", taskType, err)
	}
	return nil
}

// Module provides the enqueuer. It is wired into the API process, which records
// work, not into the worker, which runs it.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			NewClient,
			func(c *client) Enqueuer { return c },
		),
	)
}
