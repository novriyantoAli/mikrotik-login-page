package database

import (
	"go.uber.org/fx"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/service"
)

// RedisModule provides only the Redis client, for processes (the worker) that
// do not need Postgres or FreeRADIUS.
func RedisModule() fx.Option {
	return fx.Options(fx.Provide(NewRedis))
}

// Module connects to every server this system depends on and contributes one
// health checker per connection.
func Module() fx.Option {
	group := `group:"` + service.CheckersGroup + `"`

	return fx.Options(
		fx.Provide(
			NewPostgres,
			NewFreeRADIUS,
			NewRedis,
		),
		fx.Provide(
			fx.Annotate(NewPostgresChecker, fx.ResultTags(group)),
			fx.Annotate(NewFreeRADIUSChecker, fx.ResultTags(group)),
			fx.Annotate(NewRedisChecker, fx.ResultTags(group)),
		),
	)
}
