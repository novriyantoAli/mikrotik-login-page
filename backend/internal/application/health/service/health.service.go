// Package service aggregates dependency probes into one health report.
package service

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/buildinfo"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
)

// Service reports the health of this API and every dependency it needs.
type Service interface {
	Report(ctx context.Context) dto.Health
}

type healthService struct {
	checkers []Checker
	clock    clock.Clock
	timeout  time.Duration
	name     string
	logger   *zap.Logger
}

// NewService is provided by the module with every Checker injected from the
// health_checkers Fx group.
func NewService(checkers []Checker, clk clock.Clock, cfg *config.Config, logger *zap.Logger) Service {
	return &healthService{
		checkers: checkers,
		clock:    clk,
		timeout:  cfg.Health.Timeout,
		name:     cfg.App.Name,
		logger:   logger,
	}
}

// Report probes every dependency concurrently, bounding the whole probe by
// health.timeout so a hung dependency cannot hang the endpoint.
func (s *healthService) Report(ctx context.Context) dto.Health {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	checks := make(map[string]dto.Check, len(s.checkers))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, checker := range s.checkers {
		wg.Add(1)
		go func(checker Checker) {
			defer wg.Done()
			result := s.probe(ctx, checker)
			mu.Lock()
			checks[checker.Name()] = result
			mu.Unlock()
		}(checker)
	}
	wg.Wait()

	status := dto.StatusOK
	for _, check := range checks {
		if check.Status != dto.CheckUp {
			status = dto.StatusDegraded
		}
	}

	return dto.Health{
		Status:    status,
		Service:   s.name,
		Version:   buildinfo.Version,
		CheckedAt: s.clock.Now().Format(time.RFC3339),
		Checks:    checks,
	}
}

func (s *healthService) probe(ctx context.Context, checker Checker) (result dto.Check) {
	// Each probe runs in its own goroutine, outside Gin's recovery middleware,
	// so a panic in one dependency's checker must not take the process down.
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("health checker panicked",
				zap.String("dependency", checker.Name()),
				zap.Any("panic", recovered),
			)
			result = dto.Check{Status: dto.CheckDown, Error: dto.FailureUnavailable}
		}
	}()

	// A latency is a duration, so it is measured against wall time; the report's
	// checked_at uses the injected clock so tests can freeze it.
	started := time.Now()
	err := checker.Check(ctx)
	latency := time.Since(started).Milliseconds()

	if err != nil {
		// The driver's message is for operators, so it stays in the log; the
		// public response only says which category of failure happened.
		s.logger.Warn("dependency probe failed",
			zap.String("dependency", checker.Name()),
			zap.Error(err),
		)
		return dto.Check{
			Status:    dto.CheckDown,
			LatencyMS: latency,
			Error:     failureCategory(err),
		}
	}
	return dto.Check{Status: dto.CheckUp, LatencyMS: latency}
}

// failureCategory maps a dependency error onto one of the stable Failure*
// categories published by the public health endpoint.
func failureCategory(err error) string {
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, os.ErrDeadlineExceeded) {
		return dto.FailureTimeout
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return dto.FailureTimeout
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "access denied"),
		strings.Contains(message, "password authentication failed"),
		strings.Contains(message, "authentication failed"):
		return dto.FailureAuthFailed
	case strings.Contains(message, "connection refused"),
		strings.Contains(message, "no such host"),
		strings.Contains(message, "connection reset"),
		strings.Contains(message, "network is unreachable"),
		strings.Contains(message, "connect:"):
		return dto.FailureUnreachable
	default:
		return dto.FailureUnavailable
	}
}
