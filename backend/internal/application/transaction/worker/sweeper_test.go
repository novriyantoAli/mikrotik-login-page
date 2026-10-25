package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/sweep"
)

type frozenClock struct{ now time.Time }

func (c *frozenClock) Now() time.Time          { return c.now }
func (c *frozenClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// TestSweeper_HeartbeatUsesTheInjectedClock is the ADR 0018 promise that the
// worker can report "still working". A beat taken from the wall clock would never
// look stale to a checker reading the injected one, so this test advances only the
// injected clock and expects the sweeps check to fail.
func TestSweeper_HeartbeatUsesTheInjectedClock(t *testing.T) {
	t.Parallel()

	clk := &frozenClock{now: time.Date(2026, time.March, 1, 8, 30, 0, 0, time.UTC)}
	registry := sweep.NewRegistry()
	checker := sweep.NewChecker(registry, clk)

	passes := int32(0)
	sweeper := newSweeper(time.Minute, "transaction:test", registry, clk, func(context.Context) {
		atomic.AddInt32(&passes, 1)
	}, zap.NewNop())

	ctx := context.Background()
	if err := checker.Check(ctx); err == nil {
		t.Fatal("an empty registry reported healthy before the first pass")
	}

	sweeper.pass(ctx)

	if got := atomic.LoadInt32(&passes); got != 1 {
		t.Fatalf("the pass ran the job %d times, want 1", got)
	}
	if err := checker.Check(ctx); err != nil {
		t.Fatalf("a fresh heartbeat reported %v", err)
	}

	// Three intervals is the tolerance; four must be stale. Only the injected
	// clock moves, so a wall-clock heartbeat stays "fresh" and this fails.
	clk.Advance(4 * time.Minute)
	if err := checker.Check(ctx); err == nil {
		t.Fatal("a stale heartbeat reported healthy: the beat did not come from the injected clock")
	}
}

// TestSweeper_RegistersItsWakeUpTask covers the shared adapter: each sweep reacts
// to its own payload-free task, and to nothing else.
func TestSweeper_RegistersItsWakeUpTask(t *testing.T) {
	t.Parallel()

	clk := &frozenClock{now: time.Date(2026, time.March, 1, 8, 30, 0, 0, time.UTC)}
	sweeper := newSweeper(time.Minute, "transaction:test", sweep.NewRegistry(), clk,
		func(context.Context) {}, zap.NewNop())

	mux := asynq.NewServeMux()
	sweeper.register(mux, "transaction:test")

	// Asynq always returns a handler; an unregistered type gets the "not found"
	// one with an empty pattern, so the pattern is what tells them apart.
	if _, pattern := mux.Handler(asynq.NewTask("transaction:test", nil)); pattern != "transaction:test" {
		t.Fatalf("registered task resolves to pattern %q, want transaction:test", pattern)
	}
	if _, pattern := mux.Handler(asynq.NewTask("transaction:other", nil)); pattern != "" {
		t.Fatalf("the sweep answers a task it never registered (pattern %q)", pattern)
	}

	// A wake-up only nudges the loop: one request is enough, and a second must not
	// block the caller.
	sweeper.wakeUp()
	sweeper.wakeUp()
	select {
	case <-sweeper.wake:
	default:
		t.Fatal("the wake-up channel is empty after wakeUp()")
	}
	select {
	case <-sweeper.wake:
		t.Fatal("wakeUp() queued more than one pending request")
	default:
	}
}
