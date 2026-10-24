package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"

	healthdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/dto"
	notificationservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	serverworker "github.com/novriyantoAli/mikrotik-login-page/backend/internal/server/worker"
)

// fakeLifecycle records the hooks a constructor appends without starting them, so
// no test binds a port.
type fakeLifecycle struct{ hooks []fx.Hook }

func (l *fakeLifecycle) Append(hook fx.Hook) { l.hooks = append(l.hooks, hook) }

type fakeHealth struct {
	report healthdto.Health
}

func (f *fakeHealth) Report(context.Context) healthdto.Health { return f.report }

type fakeOutboxCounter struct {
	counts notificationservice.OutboxCounts
	err    error
}

func (f *fakeOutboxCounter) CountOutbox(context.Context) (notificationservice.OutboxCounts, error) {
	return f.counts, f.err
}

type fakeProvisioningCounter struct {
	counts transactionservice.ProvisioningCounts
	err    error
}

func (f *fakeProvisioningCounter) CountProvisioning(
	context.Context,
) (transactionservice.ProvisioningCounts, error) {
	return f.counts, f.err
}

// operatorHandler builds the worker's internal router the way the listener does,
// so the test drives the exact paths the compose healthcheck polls.
func operatorHandler(t *testing.T, health *fakeHealth, outbox *fakeOutboxCounter,
	provisioning *fakeProvisioningCounter,
) http.Handler {
	t.Helper()

	cfg := config.Config{Worker: config.WorkerConfig{HealthAddr: ":0"}}
	listener, err := serverworker.NewOperatorListener(
		&fakeLifecycle{}, &cfg, health, outbox, provisioning, zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("NewOperatorListener: %v", err)
	}

	handler := listener.Handler()
	if handler == nil {
		t.Fatal("the listener has no handler")
	}
	return handler
}

func healthy() *fakeHealth {
	return &fakeHealth{report: healthdto.Health{
		Status:    healthdto.StatusOK,
		Service:   "mikrotik-login-page",
		Version:   "test",
		CheckedAt: "2026-03-01T08:30:00Z",
		Checks: map[string]healthdto.Check{
			"redis":  {Status: healthdto.CheckUp},
			"sweeps": {Status: healthdto.CheckUp},
		},
	}}
}

func TestOperatorListener_HealthzMirrorsTheAPIReport(t *testing.T) {
	t.Parallel()

	handler := operatorHandler(t, healthy(), &fakeOutboxCounter{}, &fakeProvisioningCounter{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var report healthdto.Health
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Status != healthdto.StatusOK {
		t.Fatalf("status = %q, want ok", report.Status)
	}
	if _, ok := report.Checks["sweeps"]; !ok {
		t.Fatal("the report has no sweeps check: a suppressed sweep would look healthy")
	}
}

func TestOperatorListener_DegradedIsNotHealthy(t *testing.T) {
	t.Parallel()

	health := healthy()
	health.report.Status = healthdto.StatusDegraded
	health.report.Checks["sweeps"] = healthdto.Check{
		Status: healthdto.CheckDown, Error: healthdto.FailureUnavailable,
	}

	handler := operatorHandler(t, health, &fakeOutboxCounter{}, &fakeProvisioningCounter{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 so the orchestrator sees a wedged worker", recorder.Code)
	}
}

func TestOperatorListener_MetricsPublishesBothBacklogs(t *testing.T) {
	t.Parallel()

	outbox := &fakeOutboxCounter{counts: notificationservice.OutboxCounts{
		Pending: 2, Sending: 1, Sent: 120, Failed: 3, OldestPendingAgeSeconds: 421,
	}}
	provisioning := &fakeProvisioningCounter{counts: transactionservice.ProvisioningCounts{
		PaidAwaiting: 1, Attempts: 4,
	}}

	handler := operatorHandler(t, healthy(), outbox, provisioning)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	var body struct {
		Outbox struct {
			Pending                 int64 `json:"pending"`
			Sending                 int64 `json:"sending"`
			Sent                    int64 `json:"sent"`
			Failed                  int64 `json:"failed"`
			OldestPendingAgeSeconds int64 `json:"oldest_pending_age_seconds"`
		} `json:"outbox"`
		Provisioning struct {
			PaidAwaiting int64 `json:"paid_awaiting"`
			Attempts     int64 `json:"attempts"`
		} `json:"provisioning"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}

	if body.Outbox.Pending != 2 || body.Outbox.Sending != 1 || body.Outbox.Sent != 120 ||
		body.Outbox.Failed != 3 || body.Outbox.OldestPendingAgeSeconds != 421 {
		t.Fatalf("outbox body = %+v, want the counter's numbers", body.Outbox)
	}
	if body.Provisioning.PaidAwaiting != 1 || body.Provisioning.Attempts != 4 {
		t.Fatalf("provisioning body = %+v, want the counter's numbers", body.Provisioning)
	}
}

func TestOperatorListener_MetricsFailureAnswers503WithoutTheQuery(t *testing.T) {
	t.Parallel()

	outbox := &fakeOutboxCounter{err: errors.New("pq: relation \"sms_outbox\" does not exist")}
	handler := operatorHandler(t, healthy(), outbox, &fakeProvisioningCounter{})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if body := recorder.Body.String(); strings.Contains(body, "sms_outbox") {
		t.Fatalf("body %q leaks the database error", body)
	}
}

func TestOperatorListener_AnEmptyAddressDisablesTheListener(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Worker: config.WorkerConfig{HealthAddr: ""}, Server: config.ServerConfig{
		ShutdownTimeout: time.Second,
	}}
	listener, err := serverworker.NewOperatorListener(
		&fakeLifecycle{}, &cfg, healthy(), &fakeOutboxCounter{}, &fakeProvisioningCounter{},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("NewOperatorListener: %v", err)
	}
	if listener.Handler() != nil {
		t.Fatal("an empty worker.health_addr still built a listener")
	}
}
