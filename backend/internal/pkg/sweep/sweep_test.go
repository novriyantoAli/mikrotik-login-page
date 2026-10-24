package sweep_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/sweep"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

func TestChecker_NoCompletedSweepIsUnhealthy(t *testing.T) {
	t.Parallel()

	checker := sweep.NewChecker(sweep.NewRegistry(), testutil.NewFakeClock())

	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("an empty registry reported healthy: a worker that never swept has never worked")
	}
	if !strings.Contains(err.Error(), "no sweep") {
		t.Fatalf("error = %q, want it to say nothing has completed", err)
	}
}

func TestChecker_AFreshSweepIsHealthy(t *testing.T) {
	t.Parallel()

	registry := sweep.NewRegistry()
	clk := testutil.NewFakeClock()
	checker := sweep.NewChecker(registry, clk)

	registry.Beat("transaction:expire", time.Minute, clk.Now())

	if err := checker.Check(context.Background()); err != nil {
		t.Fatalf("a sweep that just finished reported %v", err)
	}

	// One missed interval is tolerated: a slow pass must not look like a stopped one.
	clk.Advance(2 * time.Minute)
	if err := checker.Check(context.Background()); err != nil {
		t.Fatalf("a sweep two intervals old reported %v", err)
	}
}

func TestChecker_AnOverdueSweepNamesItself(t *testing.T) {
	t.Parallel()

	registry := sweep.NewRegistry()
	clk := testutil.NewFakeClock()
	checker := sweep.NewChecker(registry, clk)

	registry.Beat("sms:deliver", 30*time.Second, clk.Now())
	registry.Beat("transaction:reconcile", time.Minute, clk.Now())

	// Four intervals past the SMS drainer's own schedule, but only twice the
	// reconciliation one, so exactly one name may be reported.
	clk.Advance(2 * time.Minute)

	err := checker.Check(context.Background())
	if err == nil {
		t.Fatal("an overdue sweep reported healthy")
	}
	if !strings.Contains(err.Error(), "sms:deliver") {
		t.Fatalf("error = %q, want it to name sms:deliver", err)
	}
	if strings.Contains(err.Error(), "transaction:reconcile") {
		t.Fatalf("error = %q, but reconciliation is still within its window", err)
	}
}

func TestChecker_NameIsTheGroupTheOperatorSees(t *testing.T) {
	t.Parallel()

	checker := sweep.NewChecker(sweep.NewRegistry(), testutil.NewFakeClock())

	if checker.Name() != "sweeps" {
		t.Fatalf("Name() = %q, want %q", checker.Name(), "sweeps")
	}
}
