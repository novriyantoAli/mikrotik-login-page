package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"

	healthdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/dto"
	healthservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/service"
	notificationservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

// OperatorListener is the worker's internal HTTP surface: /healthz and /metrics
// for whoever runs the host. It is deliberately not reachable through the proxy
// and serves no client contract (ADR 0018).
type OperatorListener struct {
	server *http.Server
	logger *zap.Logger
}

// metricsBody is the operator report. It is assembled here because it is an
// internal answer, not a client contract: the numbers themselves are decided by
// each domain's counter (ADR 0018).
type metricsBody struct {
	Outbox       outboxBody       `json:"outbox"`
	Provisioning provisioningBody `json:"provisioning"`
}

type outboxBody struct {
	Pending                 int64 `json:"pending"`
	Sending                 int64 `json:"sending"`
	Sent                    int64 `json:"sent"`
	Failed                  int64 `json:"failed"`
	OldestPendingAgeSeconds int64 `json:"oldest_pending_age_seconds"`
}

type provisioningBody struct {
	PaidAwaiting int64 `json:"paid_awaiting"`
	Attempts     int64 `json:"attempts"`
}

// NewOperatorListener binds the worker's internal listener. An empty address
// disables it, which is the only way to run the worker without a port.
func NewOperatorListener(
	lc fx.Lifecycle,
	cfg *config.Config,
	health healthservice.Service,
	outbox notificationservice.OutboxCounter,
	provisioning transactionservice.ProvisioningCounter,
	logger *zap.Logger,
) (*OperatorListener, error) {
	addr := cfg.Worker.HealthAddr
	if addr == "" {
		logger.Info("worker operator listener disabled")
		return &OperatorListener{logger: logger}, nil
	}

	listener := &OperatorListener{
		server: &http.Server{
			Addr:         addr,
			Handler:      newOperatorMux(health, outbox, provisioning, logger),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
		logger: logger,
	}

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			bound, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", addr, err)
			}
			logger.Info("worker operator listener started", zap.String("addr", bound.Addr().String()))

			go func() {
				if err := listener.server.Serve(bound); err != nil && !errors.Is(err, http.ErrServerClosed) {
					// A listener that dies must not leave a worker the orchestrator
					// still believes is healthy; the next healthcheck fails instead.
					logger.Error("worker operator listener stopped unexpectedly", zap.Error(err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			shutdownCtx, cancel := context.WithTimeout(ctx, cfg.Server.ShutdownTimeout)
			defer cancel()
			return listener.server.Shutdown(shutdownCtx)
		},
	})

	return listener, nil
}

// Handler exposes the internal router, or nil when the listener is disabled, so a
// test can drive the exact paths the healthcheck polls without binding a port.
func (l *OperatorListener) Handler() http.Handler {
	if l.server == nil {
		return nil
	}
	return l.server.Handler
}

// newOperatorMux maps the two paths onto the services that own the answers. The
// worker imports no Gin: this listener is not the client API.
func newOperatorMux(
	health healthservice.Service,
	outbox notificationservice.OutboxCounter,
	provisioning transactionservice.ProvisioningCounter,
	logger *zap.Logger,
) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler(health))
	mux.HandleFunc("GET /metrics", metricsHandler(outbox, provisioning, logger))
	return mux
}

// healthHandler answers like /api/health: 200 when every check is up, 503 when one
// is not, with the same report body so both surfaces mean the same thing.
func healthHandler(health healthservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := health.Report(r.Context())

		status := http.StatusOK
		if report.Status != healthdto.StatusOK {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, report)
	}
}

func metricsHandler(
	outbox notificationservice.OutboxCounter,
	provisioning transactionservice.ProvisioningCounter,
	logger *zap.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		counts, err := outbox.CountOutbox(r.Context())
		if err != nil {
			writeError(w, logger, err)
			return
		}
		provisioningCounts, err := provisioning.CountProvisioning(r.Context())
		if err != nil {
			writeError(w, logger, err)
			return
		}

		writeJSON(w, http.StatusOK, metricsBody{
			Outbox: outboxBody{
				Pending:                 counts.Pending,
				Sending:                 counts.Sending,
				Sent:                    counts.Sent,
				Failed:                  counts.Failed,
				OldestPendingAgeSeconds: counts.OldestPendingAgeSeconds,
			},
			Provisioning: provisioningBody{
				PaidAwaiting: provisioningCounts.PaidAwaiting,
				Attempts:     provisioningCounts.Attempts,
			},
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The response is already committed; the client sees a short body.
		return
	}
}

// writeError keeps the database's own text in the log: whoever can reach the port
// gets the category, not the query.
func writeError(w http.ResponseWriter, logger *zap.Logger, err error) {
	logger.Warn("worker metrics unavailable", zap.Error(err))
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "metrics unavailable"})
}
