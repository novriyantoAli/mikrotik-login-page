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

// Module connects to every server this system depends on, contributes one
// health checker per connection, and provides the vendor adapters that live
// beside those connections.
func Module() fx.Option {
	group := `group:"` + service.CheckersGroup + `"`

	return fx.Options(
		PostgresModule(),
		fx.Provide(
			NewFreeRADIUS,
			NewRedis,
			fx.Annotate(
				NewRadiusExpirationReader,
				fx.As(new(provisioning.ExpirationReader)),
			),
			// Rate limits live in Redis, which is already a hard dependency
			// (Asynq), so Magic Link limits add no infrastructure (ADR 0011).
			ratelimit.NewRedisLimiter,
		),
		fx.Provide(
			fx.Annotate(NewPostgresChecker, fx.ResultTags(group)),
			fx.Annotate(NewFreeRADIUSChecker, fx.ResultTags(group)),
			fx.Annotate(NewRedisChecker, fx.ResultTags(group)),
		),
	)
}
