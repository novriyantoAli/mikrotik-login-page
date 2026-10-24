package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
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
		_, err := s.transitions.settle(ctx, row, callback)
		return err
	case payment.StatusFailed, payment.StatusExpired:
		_, err := s.transitions.close(ctx, row, callback.Status)
		return err
	default:
		// "unpaid" is the normal state of a checkout nobody has paid yet.
		return nil
	}
}
