package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/queue"
)

// Reconciler recovers a settlement whose callback was lost. It is a separate
// surface from Service because only the worker process runs it, and it is a
// sweep rather than a per-Transaction task because the durable work item is
// "every pending Transaction that is still payable" (ADR 0018).
type Reconciler interface {
	// ReconcileDue asks the gateway about the pending Transactions still payable
	// at this moment and applies the answer through the same settle/close path a
	// callback uses. It reports how many rows this sweep moved.
	ReconcileDue(ctx context.Context) (int, error)
}

type reconciler struct {
	repo        repository.Repository
	gateway     payment.Gateway
	transitions *transitions
	clock       clock.Clock
	settings    SweepSettings
	logger      *zap.Logger
}

// NewReconciler builds the reconciliation sweep; Fx injects every port, so the
// gateway stays replaceable in tests.
func NewReconciler(
	repo repository.Repository,
	gateway payment.Gateway,
	waker queue.Enqueuer,
	clock clock.Clock,
	settings SweepSettings,
	logger *zap.Logger,
) Reconciler {
	return &reconciler{
		repo:        repo,
		gateway:     gateway,
		transitions: newTransitions(repo, waker, clock, logger),
		clock:       clock,
		settings:    settings,
		logger:      logger,
	}
}

func (r *reconciler) ReconcileDue(ctx context.Context) (int, error) {
	now := r.clock.Now()

	// Only rows whose deadline can still be met are asked about: `expired` is
	// final (ADR 0015), so a settlement arriving after it would be logged and
	// dropped. That makes the selector the reconciliation window (ADR 0018).
	ids, err := r.repo.ReconcilableIDs(ctx, now, r.settings.Reconcile.Batch)
	if err != nil {
		return 0, err
	}

	reconciled := 0
	for _, id := range ids {
		changed, err := r.reconcile(ctx, id, now)
		if err != nil {
			// One row must not stop the sweep: the others still deserve an answer,
			// and a failing row stays pending for the next tick.
			r.logger.Error("reconcile pending transaction",
				zap.Uint("transaction_id", id), zap.Error(err))
			continue
		}
		if changed {
			reconciled++
		}
	}
	return reconciled, nil
}

// reconcile asks the gateway about one Transaction and applies the answer. It
// returns whether the row moved; an unpaid row, an unreachable gateway and a
// mismatch all leave it pending.
func (r *reconciler) reconcile(ctx context.Context, id uint, now time.Time) (bool, error) {
	row, err := r.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, err
	}

	// The row may have been settled or closed between the select and this read;
	// re-checking here keeps the gateway call to payments that still need one.
	if row.State != entity.StatePending || !row.ExpiresAt.After(now) {
		return false, nil
	}

	status, err := r.gateway.GetStatus(ctx, row.Reference)
	if err != nil {
		// A status read is not authoritative, so a failure changes nothing: the
		// callback path is still open and the deadline still runs (ADR 0018).
		r.logger.Warn("reconciliation could not read the gateway status",
			zap.String("reference", row.Reference), zap.Error(err))
		return false, nil
	}
	if status == nil {
		return false, nil
	}

	switch status.Status {
	case payment.StatusPaid:
		callback := &payment.Callback{
			Reference: row.Reference,
			Status:    status.Status,
			Amount:    status.Amount,
			PaidAt:    status.PaidAt,
		}
		changed, err := r.transitions.settle(ctx, row, callback, ReasonReconciliation)
		if err != nil {
			if errors.Is(err, ErrAmountMismatch) {
				// Already logged with both amounts by settle; the row stays pending
				// until an operator reconciles the money (ADR 0015).
				return false, nil
			}
			return false, fmt.Errorf("settle reconciled payment: %w", err)
		}
		return changed, nil
	case payment.StatusFailed, payment.StatusExpired:
		return r.transitions.close(ctx, row, status.Status, ReasonReconciliation)
	default:
		// "unpaid" is the expected answer for a checkout nobody has paid yet.
		return false, nil
	}
}
