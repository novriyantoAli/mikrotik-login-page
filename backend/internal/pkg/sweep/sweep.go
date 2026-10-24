// Package sweep tracks when each periodic job last finished. A worker with a
// working Redis connection and a reachable database can still have stopped
// sweeping, and that is the failure a dependency probe cannot see (ADR 0018).
package sweep

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
)

// stalenessFactor is how many intervals a sweep may miss before it counts as
// stopped. Three tolerates a slow pass without hiding a wedged one.
const stalenessFactor = 3

// Registry holds the last completed pass of every periodic job in this process.
// Names are the job's own vocabulary ("transaction:expire"), so the health line
// names the thing that stopped.
type Registry struct {
	mu    sync.Mutex
	beats map[string]beat
}

type beat struct {
	interval time.Duration
	at       time.Time
}

// NewRegistry builds an empty registry; Fx provides one per process.
func NewRegistry() *Registry {
	return &Registry{beats: make(map[string]beat)}
}

// Beat records that `name` just completed a pass. interval is how often that name
// is expected to run; the checker derives its own window from it, so this package
// never needs a second copy of the schedule.
func (r *Registry) Beat(name string, interval time.Duration, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.beats[name] = beat{interval: interval, at: at}
}

func (r *Registry) snapshot() map[string]beat {
	r.mu.Lock()
	defer r.mu.Unlock()

	copied := make(map[string]beat, len(r.beats))
	for name, value := range r.beats {
		copied[name] = value
	}
	return copied
}

// Checker reports the sweeps as one health check named "sweeps".
type Checker struct {
	registry *Registry
	clock    clock.Clock
}

// NewChecker builds the health check over a registry. It is contributed to the
// health_checkers Fx group, so the worker's /healthz carries it next to the
// dependency probes without any transport knowing about it.
func NewChecker(registry *Registry, clk clock.Clock) *Checker {
	return &Checker{registry: registry, clock: clk}
}

// Name implements health.Checker.
func (c *Checker) Name() string { return "sweeps" }

// Check implements health.Checker. It fails when a periodic job is overdue or
// when nothing has completed yet: a worker that never swept has never worked,
// and the compose healthcheck's start period is what covers the first pass.
func (c *Checker) Check(context.Context) error {
	beats := c.registry.snapshot()
	if len(beats) == 0 {
		return errors.New("no sweep has completed yet")
	}

	now := c.clock.Now()
	stale := make([]string, 0, len(beats))
	for name, value := range beats {
		if now.Sub(value.at) > stalenessFactor*value.interval {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		return fmt.Errorf("stale sweeps: %s", strings.Join(stale, ", "))
	}
	return nil
}
