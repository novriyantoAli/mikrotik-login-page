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

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/activeperiod"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/auth"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification"
	notificationworker "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/worker"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment/tripay"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction"
	transactionworker "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/worker"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/logger"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/modem"
	serverapi "github.com/novriyantoAli/mikrotik-login-page/backend/internal/server/api"
	servermigration "github.com/novriyantoAli/mikrotik-login-page/backend/internal/server/migration"
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

// API is the HTTP process: Postgres, MySQL, Redis, the catalog and Active
// Period reads, the health domain and the router.
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
		packages.Module(),
		activeperiod.Module(),
		notification.Module(),
		auth.Module(),
		tripay.Module(),
		transaction.Module(),
		serverapi.RouterModule(),
		fx.Options(extra...),
	)
}

// Migrate is the deployment migration process: Postgres, the Package catalog
// domain and the migration runner. It applies the schema and the seed, then
// exits; the API and worker never migrate themselves (ADR 0012).
func Migrate(extra ...fx.Option) fx.Option {
	return fx.Options(
		Core(),
		database.PostgresModule(),
		packages.Module(),
		servermigration.Module(),
		fx.Options(extra...),
	)
}

// Worker is the background process. It owns Redis today (Asynq plus the startup
// liveness ping); domains add the connections they need (Postgres, FreeRADIUS) as
// they are built, so the worker never depends on a server it does not use.
func Worker(extra ...fx.Option) fx.Option {
	return WorkerWithBase(Base(), extra...)
}

// WorkerWithBase builds the background process on an explicit base. The worker
// owns the modem, so it is the only process that builds the SMS drainer and the
// vendor adapter (ADR 0010).
func WorkerWithBase(base fx.Option, extra ...fx.Option) fx.Option {
	return fx.Options(
		base,
		database.RedisModule(),
		database.PostgresModule(),
		// Provisioning writes FreeRADIUS from this process, so the worker owns the
		// MySQL connection and its adapters (ADR 0016).
		database.FreeRADIUSModule(),
		packages.Module(),
		notification.WorkerModule(),
		transaction.WorkerModule(),
		modem.Module(),
		serverworker.Module(),
		fx.Provide(
			// Fx cannot inject a bare time.Duration, so the drainer gets the one
			// knob it needs in a local settings struct.
			func(cfg *config.Config) notificationworker.Settings {
				return notificationworker.Settings{SweepInterval: cfg.SMS.SweepInterval}
			},
			func(cfg *config.Config) transactionworker.Settings {
				return transactionworker.Settings{SweepInterval: cfg.Transaction.ExpirySweepInterval}
			},
			fx.Annotate(
				notificationworker.NewDrainer,
				fx.As(new(serverworker.TaskRegistrar)),
				fx.ResultTags(`group:"worker_tasks"`),
			),
			fx.Annotate(
				transactionworker.NewExpiry,
				fx.As(new(serverworker.TaskRegistrar)),
				fx.ResultTags(`group:"worker_tasks"`),
			),
			fx.Annotate(
				transactionworker.NewProvisioning,
				fx.As(new(serverworker.TaskRegistrar)),
				fx.ResultTags(`group:"worker_tasks"`),
			),
		),
		fx.Options(extra...),
	)
}

// Run starts the graph, blocks until a signal arrives and then stops it
// gracefully. A clean SIGTERM/SIGINT returns nil so the process exits 0.
func Run(options fx.Option) error {
	application := newApp(options)

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

// RunOnce starts the graph so its invokes run and then stops it. Commands that
// do one job and exit (migration) use it instead of Run, which waits for a
// signal that would never come.
func RunOnce(options fx.Option) error {
	application := newApp(options)

	startCtx, cancelStart := context.WithTimeout(context.Background(), startTimeout)
	defer cancelStart()
	if err := application.Start(startCtx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), stopTimeout)
	defer cancelStop()
	if err := application.Stop(stopCtx); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	return nil
}

func newApp(options fx.Option) *fx.App {
	return fx.New(
		options,
		fx.StartTimeout(startTimeout),
		fx.StopTimeout(stopTimeout),
		// Fx's own event stream is diagnostic noise in normal operation; keep
		// it out of the structured logs unless something goes wrong.
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log.WithOptions(zap.IncreaseLevel(zap.WarnLevel))}
		}),
	)
}
