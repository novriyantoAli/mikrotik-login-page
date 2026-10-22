// Package app is the composition root shared by the api and worker binaries and
// by integration tests. Every process builds the same base graph; the command
// decides which transport (and therefore which hardware) it owns.
package app

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/logger"
	serverapi "github.com/novriyantoAli/mikrotik-login-page/backend/internal/server/api"
	serverworker "github.com/novriyantoAli/mikrotik-login-page/backend/internal/server/worker"
)

// Fx lifecycle budgets. They are process budgets, not deployment values: the
// graceful HTTP timeout that matters to operators is server.shutdown_timeout.
const (
	startTimeout = 30 * time.Second
	stopTimeout  = 30 * time.Second
)

// Core is the provider set every process needs before any transport:
// configuration and logging. It leaves the clock out so tests can inject a
// frozen clock without decorating the graph.
func Core() fx.Option {
	return fx.Options(
		fx.Provide(
			config.Load,
			logger.New,
		),
	)
}

// Base is Core plus the production wall clock.
func Base() fx.Option {
	return fx.Options(
		Core(),
		fx.Provide(clock.New),
	)
}

// API is the HTTP process: Postgres, MySQL, Redis, the health domain and the
// router.
func API(extra ...fx.Option) fx.Option {
	return APIWithBase(Base(), extra...)
}

// APIWithBase builds the HTTP process on an explicit base, so tests can swap
// the clock while keeping every other provider identical to production.
func APIWithBase(base fx.Option, extra ...fx.Option) fx.Option {
	return fx.Options(
		routerWithBase(base, extra...),
		serverapi.ListenerModule(),
	)
}

// RouterWithBase builds the same graph as APIWithBase without binding a port.
// Integration tests drive the production router through httptest.
func RouterWithBase(base fx.Option, extra ...fx.Option) fx.Option {
	return routerWithBase(base, extra...)
}

func routerWithBase(base fx.Option, extra ...fx.Option) fx.Option {
	return fx.Options(
		base,
		database.Module(),
		health.Module(),
		serverapi.RouterModule(),
		fx.Options(extra...),
	)
}

// Worker is the background process. It owns Redis today (Asynq plus the startup
// liveness ping); domains add the connections they need (Postgres, FreeRADIUS) as
// they are built, so the worker never depends on a server it does not use.
func Worker(extra ...fx.Option) fx.Option {
	return WorkerWithBase(Base(), extra...)
}

// WorkerWithBase builds the background process on an explicit base.
func WorkerWithBase(base fx.Option, extra ...fx.Option) fx.Option {
	return fx.Options(
		base,
		database.RedisModule(),
		serverworker.Module(),
		fx.Options(extra...),
	)
}

// Run starts the graph, blocks until a signal arrives and then stops it
// gracefully. A clean SIGTERM/SIGINT returns nil so the process exits 0.
func Run(options fx.Option) error {
	application := fx.New(
		options,
		fx.StartTimeout(startTimeout),
		fx.StopTimeout(stopTimeout),
		// Fx's own event stream is diagnostic noise in normal operation; keep
		// it out of the structured logs unless something goes wrong.
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log.WithOptions(zap.IncreaseLevel(zap.WarnLevel))}
		}),
	)

	startCtx, cancelStart := context.WithTimeout(context.Background(), startTimeout)
	defer cancelStart()
	if err := application.Start(startCtx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	<-application.Done()

	stopCtx, cancelStop := context.WithTimeout(context.Background(), stopTimeout)
	defer cancelStop()
	if err := application.Stop(stopCtx); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	return nil
}
