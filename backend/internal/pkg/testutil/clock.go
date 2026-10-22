// Package testutil is the integration-test harness: real Postgres with a fresh
// schema per test, a real Redis, and fakes for every vendor boundary. Tests
// assert behavior through the HTTP contract, never through internal calls.
package testutil

import (
	"sync"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
)

// FakeClock is a clock the test controls. Domain code only sees the Clock
// interface; tests move time explicitly instead of sleeping.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock freezes time at start (UTC).
func NewFakeClock() *FakeClock {
	return &FakeClock{now: time.Date(2026, time.March, 1, 8, 30, 0, 0, time.UTC)}
}

// Now implements clock.Clock.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to an absolute instant.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t.UTC()
}

// Advance moves the clock forward (or backward, for negative durations).
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var _ clock.Clock = (*FakeClock)(nil)
