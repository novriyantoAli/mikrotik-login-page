package database

import (
	"go.uber.org/fx"
	"gorm.io/gorm"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/ratelimit"
)

// RedisModule provides only the Redis client, for processes (the worker) that
// do not need Postgres or FreeRADIUS.
func RedisModule() fx.Option {
	return fx.Options(fx.Provide(NewRedis))
}

// PostgresModule provides the Postgres connection and the GORM handle
// repositories depend on, for processes (the migration command) that touch no
// other server.
func PostgresModule() fx.Option {
	return fx.Options(
		fx.Provide(
			NewPostgres,
			// Repositories take *gorm.DB (and only that), so the Fx graph must
			// offer it as its own type instead of the Postgres wrapper.
			func(postgres *Postgres) *gorm.DB { return postgres.Gorm },
		),
	)
}

// FreeRADIUSModule provides the FreeRADIUS connection and the adapters that live
// beside it: the Active Period read (ADR 0014) and the account write (ADR 0016).
// The worker builds it without serving HTTP; the API builds it for its health
// check and for the provisioning surface integration tests drive.
func FreeRADIUSModule() fx.Option {
	return fx.Options(
		fx.Provide(
			NewFreeRADIUS,
			fx.Annotate(
				NewRadiusExpirationReader,
				fx.As(new(provisioning.ExpirationReader)),
			),
			fx.Annotate(
				NewRadiusWriter,
				fx.As(new(provisioning.Writer)),
			),
		),
	)
}

// CheckersModule contributes one health checker per connection. It is separate
// from Module because the worker builds its servers one at a time yet must
// report them in the same shape as the API: /healthz and /api/health have to
// agree about what "the database is down" means (ADR 0018).
func CheckersModule() fx.Option {
	group := `group:"` + service.CheckersGroup + `"`

	return fx.Options(
		fx.Provide(
			fx.Annotate(NewPostgresChecker, fx.ResultTags(group)),
			fx.Annotate(NewFreeRADIUSChecker, fx.ResultTags(group)),
			fx.Annotate(NewRedisChecker, fx.ResultTags(group)),
		),
	)
}

// Module connects to every server this system depends on, contributes one
// health checker per connection, and provides the vendor adapters that live
// beside those connections.
func Module() fx.Option {
	return fx.Options(
		PostgresModule(),
		FreeRADIUSModule(),
		fx.Provide(
			NewRedis,
			// Rate limits live in Redis, which is already a hard dependency
			// (Asynq), so Magic Link limits add no infrastructure (ADR 0011).
			ratelimit.NewRedisLimiter,
		),
		CheckersModule(),
	)
}
