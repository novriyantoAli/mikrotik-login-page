// Package dto holds the only types that cross the HTTP boundary for the health
// endpoint (golden rule: entities never reach JSON).
package dto

// Overall status values. "ok" means every dependency is reachable, "degraded"
// means at least one is not.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
)

// Per-dependency status values.
const (
	CheckUp   = "up"
	CheckDown = "down"
)

// Failure categories published for a dependency that is down. They are stable
// contract values: the driver's own error text stays in the log, never in the
// public response body.
const (
	FailureTimeout     = "timeout"
	FailureUnreachable = "unreachable"
	FailureAuthFailed  = "auth_failed"
	FailureUnavailable = "unavailable"
)

// Check is the result of probing one dependency. Error is one of the Failure*
// categories, present only when Status is "down".
type Check struct {
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// Health is the body of GET /api/health. CheckedAt is an RFC 3339 timestamp
// from the injected clock so tests can freeze it.
type Health struct {
	Status    string           `json:"status"`
	Service   string           `json:"service"`
	Version   string           `json:"version"`
	CheckedAt string           `json:"checked_at"`
	Checks    map[string]Check `json:"checks"`
}
