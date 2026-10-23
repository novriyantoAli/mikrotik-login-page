package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
)

// Expiry is the background half of the payment deadline: a pending Transaction
// whose deadline passed is expired by the worker, never by the client still
// holding the page open (PRD point 20). It is a separate surface from Service so
// the worker process does not have to build a payment gateway.
type Expiry interface {
	// ExpireDue closes every overdue pending Transaction and reports how many it
	// changed. Running it twice is harmless.
	ExpireDue(ctx context.Context) (int64, error)
}

type expiry struct {
	repo   repository.Repository
	clock  clock.Clock
	logger *zap.Logger
}

// NewExpiry builds the expiry sweeper; Fx injects the repository and the clock.
func NewExpiry(repo repository.Repository, clock clock.Clock, logger *zap.Logger) Expiry {
	return &expiry{repo: repo, clock: clock, logger: logger}
}

func (e *expiry) ExpireDue(ctx context.Context) (int64, error) {
	expired, err := e.repo.ExpireOverdue(ctx, e.clock.Now())
	if err != nil {
		return 0, fmt.Errorf("expire overdue transactions: %w", err)
	}
	if expired > 0 {
		e.logger.Info("pending transactions expired", zap.Int64("count", expired))
	}
	return expired, nil
}
