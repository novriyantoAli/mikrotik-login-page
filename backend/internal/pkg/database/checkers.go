package database

import (
	"context"

	"github.com/redis/go-redis/v9"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/service"
)

// Dependency names as they appear in the /api/health response. The frontend and
// the shared contract fixture key off these strings, so they are part of the
// contract, not an implementation detail.
const (
	CheckPostgres        = "postgres"
	CheckFreeRADIUSMySQL = "freeradius_mysql"
	CheckRedis           = "redis"
)

// pingChecker adapts a ping function to the health Checker port.
type pingChecker struct {
	name string
	ping func(ctx context.Context) error
}

func (c pingChecker) Name() string { return c.name }

func (c pingChecker) Check(ctx context.Context) error { return c.ping(ctx) }

// NewPostgresChecker probes the application database.
func NewPostgresChecker(pg *Postgres) service.Checker {
	return pingChecker{name: CheckPostgres, ping: pg.SQL.PingContext}
}

// NewFreeRADIUSChecker probes the FreeRADIUS database.
func NewFreeRADIUSChecker(radius *FreeRADIUS) service.Checker {
	return pingChecker{name: CheckFreeRADIUSMySQL, ping: radius.DB.PingContext}
}

// NewRedisChecker probes Redis by writing a round-trip PING.
func NewRedisChecker(client *redis.Client) service.Checker {
	return pingChecker{name: CheckRedis, ping: func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}}
}
