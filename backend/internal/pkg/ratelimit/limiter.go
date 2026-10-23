// Package ratelimit bounds repeated requests per arbitrary key. Magic Link
// requests are limited per Phone Number and per client IP (ADR 0011), so the
// port is narrow: "may this key act once more inside this window?".
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Limiter decides whether one more request may go through.
//
// Allow returns false when the caller is over its limit. It never replaces a
// storage failure with "allowed": an unreachable Redis is an error the caller
// must decide about, because silently dropping the rate limit is exactly the
// abuse the limit exists to stop.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// The fixed-window counter is one atomic round trip: increment, and let the
// first increment of a window set the expiry.
var allowScript = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return count
`)

// RedisLimiter is the production Limiter. Redis is already a hard dependency
// (Asynq), so the counters cost no new infrastructure (ADR 0011).
type RedisLimiter struct {
	client *redis.Client
	logger *zap.Logger
}

// NewRedisLimiter builds the Redis-backed limiter; Fx injects the client.
func NewRedisLimiter(client *redis.Client, logger *zap.Logger) Limiter {
	return &RedisLimiter{client: client, logger: logger}
}

// Allow implements Limiter.
func (l *RedisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if limit <= 0 || window <= 0 {
		// A misconfigured limit must not silently become "block everyone" or
		// "block nobody": validate() already rejects it, so treat it as a bug.
		return false, fmt.Errorf("rate limit needs a positive limit and window")
	}

	count, err := allowScript.Run(ctx, l.client, []string{key}, window.Milliseconds()).Int64()
	if err != nil {
		l.logger.Error("rate limit counter",
			zap.String("key", key),
			zap.Error(err),
		)
		return false, fmt.Errorf("rate limit %s: %w", key, err)
	}
	return count <= int64(limit), nil
}
