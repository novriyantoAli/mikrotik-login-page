package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

type fakeChecker struct {
	name  string
	err   error
	delay time.Duration
}

type panicChecker struct{ name string }

func (c panicChecker) Name() string { return c.name }

func (panicChecker) Check(context.Context) error { panic("checker exploded") }

func (c fakeChecker) Name() string { return c.name }

func (c fakeChecker) Check(ctx context.Context) error {
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return c.err
}

func newService(t *testing.T, timeout time.Duration, checkers ...service.Checker) (service.Service, *testutil.FakeClock) {
	t.Helper()

	fakeClock := testutil.NewFakeClock()
	cfg := &config.Config{
		App:    config.AppConfig{Name: "test-service"},
		Health: config.HealthConfig{Timeout: timeout},
	}

	return service.NewService(checkers, fakeClock, cfg, zap.NewNop()), fakeClock
}

func TestReport_AllDependenciesUp(t *testing.T) {
	svc, fakeClock := newService(t, time.Second,
		fakeChecker{name: "postgres"},
		fakeChecker{name: "redis"},
	)

	report := svc.Report(context.Background())

	if report.Status != dto.StatusOK {
		t.Fatalf("status = %q, want %q", report.Status, dto.StatusOK)
	}
	if report.Service != "test-service" {
		t.Fatalf("service = %q", report.Service)
	}
	if report.CheckedAt != fakeClock.Now().Format(time.RFC3339) {
		t.Fatalf("checked_at = %q, want the injected clock", report.CheckedAt)
	}
	for name, check := range report.Checks {
		if check.Status != dto.CheckUp {
			t.Fatalf("check %q = %+v", name, check)
		}
	}
}

func TestReport_DegradedDependencyExplainsItself(t *testing.T) {
	svc, _ := newService(t, time.Second,
		fakeChecker{name: "postgres"},
		fakeChecker{name: "freeradius_mysql", err: errors.New("connection refused")},
	)

	report := svc.Report(context.Background())

	if report.Status != dto.StatusDegraded {
		t.Fatalf("status = %q, want %q", report.Status, dto.StatusDegraded)
	}
	failing := report.Checks["freeradius_mysql"]
	if failing.Status != dto.CheckDown {
		t.Fatalf("freeradius_mysql = %+v", failing)
	}
	if failing.Error != dto.FailureUnreachable {
		t.Fatalf("error = %q, want the %q category", failing.Error, dto.FailureUnreachable)
	}
	if report.Checks["postgres"].Status != dto.CheckUp {
		t.Fatalf("postgres = %+v", report.Checks["postgres"])
	}
}

func TestReport_PanicInOneCheckerDoesNotKillTheReport(t *testing.T) {
	svc, _ := newService(t, time.Second,
		fakeChecker{name: "postgres"},
		panicChecker{name: "redis"},
	)

	report := svc.Report(context.Background())

	if report.Status != dto.StatusDegraded {
		t.Fatalf("status = %q, want %q", report.Status, dto.StatusDegraded)
	}
	if report.Checks["redis"].Status != dto.CheckDown {
		t.Fatalf("redis = %+v", report.Checks["redis"])
	}
	if report.Checks["redis"].Error != dto.FailureUnavailable {
		t.Fatalf("redis error = %q, want %q", report.Checks["redis"].Error, dto.FailureUnavailable)
	}
	if report.Checks["postgres"].Status != dto.CheckUp {
		t.Fatalf("postgres = %+v", report.Checks["postgres"])
	}
}

func TestReport_BoundsAHungDependency(t *testing.T) {
	svc, _ := newService(t, 100*time.Millisecond,
		fakeChecker{name: "redis", delay: 5 * time.Second},
	)

	started := time.Now()
	report := svc.Report(context.Background())
	elapsed := time.Since(started)

	if elapsed > 2*time.Second {
		t.Fatalf("report took %v, health.timeout was not applied", elapsed)
	}
	if report.Checks["redis"].Status != dto.CheckDown {
		t.Fatalf("redis = %+v", report.Checks["redis"])
	}
	if report.Status != dto.StatusDegraded {
		t.Fatalf("status = %q, want %q", report.Status, dto.StatusDegraded)
	}
}
