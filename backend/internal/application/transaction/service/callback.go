package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/repository"
)

// HandleCallback applies the authoritative settlement signal (ADR 0008). It is
// the only path into `paid`, and it is idempotent: a repeated notification loses
// the guarded UPDATE and changes nothing, so provisioning (#15) cannot be
// triggered twice (PRD point 68).
func (s *service) HandleCallback(ctx context.Context, raw []byte, signature string) error {
	callback, err := s.gateway.VerifyCallback(raw, signature)
	if err != nil {
		// An unsigned or unresolvable notification is a security event: it is
		// logged without its body, because the body is attacker-controlled.
		s.logger.Warn("gateway callback rejected", zap.Error(err))
		return fmt.Errorf("verify callback: %w", err)
	}

	row, err := s.repo.GetByReference(ctx, callback.Reference)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// A verified notification for a reference this system never issued:
			// acknowledge it so the gateway stops retrying, and make it visible.
			s.logger.Warn("gateway callback names an unknown reference",
				zap.String("reference", callback.Reference),
			)
			return nil
		}
		return err
	}

	switch callback.Status {
	case payment.StatusPaid:
		return s.settle(ctx, row, callback)
	case payment.StatusFailed, payment.StatusExpired:
		return s.close(ctx, row, callback.Status)
	default:
		// "unpaid" is the normal state of a checkout nobody has paid yet.
		return nil
	}
}

func (s *service) settle(ctx context.Context, row *entity.Transaction, callback *payment.Callback) error {
	if callback.Amount != row.AmountIDR {
		s.logger.Error("gateway callback amount does not match the Transaction",
			zap.String("reference", row.Reference),
			zap.Int64("expected_amount_idr", row.AmountIDR),
			zap.Int64("received_amount_idr", callback.Amount),
		)
		return ErrAmountMismatch
	}

	if row.State == entity.StatePaid || row.State == entity.StateProvisioned {
		s.logger.Info("duplicate settlement callback ignored",
			zap.String("reference", row.Reference),
			zap.String("state", string(row.State)),
		)
		return nil
	}
	if !row.State.CanTransitionTo(entity.StatePaid) {
		// The deadline already closed this Transaction. Marking it paid would be a
		// transition the state machine forbids; it needs an operator, not a rule
		// break in the callback path.
		s.logger.Warn("settlement arrived for a Transaction that can no longer be paid",
			zap.String("reference", row.Reference),
			zap.String("state", string(row.State)),
		)
		return nil
	}

	paidAt := s.clock.Now()
	if callback.PaidAt != nil {
		paidAt = callback.PaidAt.UTC()
	}

	changed, err := s.repo.Transition(ctx, row.ID, entity.StatePending, entity.StatePaid, paidAt)
	if err != nil {
		return err
	}
	if !changed {
		s.logger.Info("duplicate settlement callback lost the transition race",
			zap.String("reference", row.Reference),
		)
		return nil
	}

	s.logger.Info("transaction paid",
		zap.String("reference", row.Reference),
		zap.Int64("amount_idr", row.AmountIDR),
	)
	return nil
}

func (s *service) close(ctx context.Context, row *entity.Transaction, status payment.Status) error {
	next := entity.StateFailed
	if status == payment.StatusExpired {
		next = entity.StateExpired
	}
	if !row.State.CanTransitionTo(next) {
		s.logger.Info("callback cannot close a Transaction in this state",
			zap.String("reference", row.Reference),
			zap.String("state", string(row.State)),
			zap.String("next", string(next)),
		)
		return nil
	}

	changed, err := s.repo.Transition(ctx, row.ID, entity.StatePending, next, s.clock.Now())
	if err != nil {
		return err
	}
	if changed {
		s.logger.Info("transaction closed by the gateway",
			zap.String("reference", row.Reference),
			zap.String("state", string(next)),
		)
	}
	return nil
}
