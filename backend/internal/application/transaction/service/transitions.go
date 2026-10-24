package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/clock"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/queue"
)

// transitions applies the two state changes a settlement can cause: pending →
// paid, which also wakes provisioning, and pending → failed/expired. It is a
// small value built over the same ports by both surfaces that need it — the
// gateway callback and the reconciliation sweep — so a lost callback and a live
// one cannot take different paths into `paid` (ADR 0015, ADR 0018).
type transitions struct {
	repo   repository.Repository
	waker  queue.Enqueuer
	clock  clock.Clock
	logger *zap.Logger
}

func newTransitions(
	repo repository.Repository,
	waker queue.Enqueuer,
	clk clock.Clock,
	logger *zap.Logger,
) *transitions {
	return &transitions{repo: repo, waker: waker, clock: clk, logger: logger}
}

// settle applies a verified settlement. changed reports whether this call moved
// the row: a repeat, a lost race and a payment for a Transaction that already
// reached a final state are all acknowledged without an error, because the
// caller is a notification that must not be retried forever.
func (t *transitions) settle(
	ctx context.Context,
	row *entity.Transaction,
	callback *payment.Callback,
) (bool, error) {
	if callback.Amount != row.AmountIDR {
		t.logger.Error("settlement amount does not match the Transaction",
			zap.String("reference", row.Reference),
			zap.Int64("expected_amount_idr", row.AmountIDR),
			zap.Int64("received_amount_idr", callback.Amount),
		)
		return false, ErrAmountMismatch
	}

	if row.State == entity.StatePaid || row.State == entity.StateProvisioned {
		t.logger.Info("duplicate settlement ignored",
			zap.String("reference", row.Reference),
			zap.String("state", string(row.State)),
		)
		return false, nil
	}
	if !row.State.CanTransitionTo(entity.StatePaid) {
		// The deadline already closed this Transaction. Marking it paid would be a
		// transition the state machine forbids; it needs an operator, not a rule
		// break in the callback path.
		t.logger.Warn("settlement arrived for a Transaction that can no longer be paid",
			zap.String("reference", row.Reference),
			zap.String("state", string(row.State)),
		)
		return false, nil
	}

	paidAt := t.clock.Now()
	if callback.PaidAt != nil {
		paidAt = callback.PaidAt.UTC()
	}

	changed, err := t.repo.Transition(ctx, row.ID, entity.StatePending, entity.StatePaid, paidAt)
	if err != nil {
		return false, err
	}
	if !changed {
		t.logger.Info("duplicate settlement lost the transition race",
			zap.String("reference", row.Reference),
		)
		return false, nil
	}

	t.logger.Info("transaction paid",
		zap.String("reference", row.Reference),
		zap.Int64("amount_idr", row.AmountIDR),
	)
	// Ask the worker to provision now; the paid row is the durable work item, so a
	// lost wake-up costs one sweep interval, not the account (ADR 0016).
	if err := t.waker.Enqueue(ctx, TaskProvision); err != nil {
		t.logger.Warn("settlement recorded without a provisioning wake-up", zap.Error(err))
	}
	return true, nil
}

// close moves a pending Transaction to the state the gateway reported and
// reports whether it changed. `failed` and `expired` are final, so a repeated
// notification finds nothing to do (ADR 0015).
func (t *transitions) close(
	ctx context.Context,
	row *entity.Transaction,
	status payment.Status,
) (bool, error) {
	next := entity.StateFailed
	if status == payment.StatusExpired {
		next = entity.StateExpired
	}
	if !row.State.CanTransitionTo(next) {
		t.logger.Info("settlement cannot close a Transaction in this state",
			zap.String("reference", row.Reference),
			zap.String("state", string(row.State)),
			zap.String("next", string(next)),
		)
		return false, nil
	}

	changed, err := t.repo.Transition(ctx, row.ID, entity.StatePending, next, t.clock.Now())
	if err != nil {
		return false, err
	}
	if changed {
		t.logger.Info("transaction closed by the gateway",
			zap.String("reference", row.Reference),
			zap.String("state", string(next)),
		)
	}
	return changed, nil
}
