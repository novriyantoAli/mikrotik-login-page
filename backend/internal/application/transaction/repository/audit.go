// This file is the Transaction domain's audit edge: the one row a PIN reveal owes.
// It lives here, beside the repository, because "the entry is written inside a
// transaction" is a property of the writer, not of the service that decided the fact
// (ADR 0027 §2).
package repository

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// RecordPINReveal appends exactly one trail row for one reveal attempt. The attempt
// is the interesting fact whether or not it produced a PIN, so the service calls this
// on both paths; the entry it hands in carries no plaintext (ADR 0027 §4).
//
// A reveal changes no row, so there is nothing to commit the entry with. It gets its
// own transaction anyway: `WithinTx` is the only constructor of a writable handle, and
// the recorder refuses a zero one, which is what keeps an entry from committing
// outside a transaction at all (ADR 0027 §2).
func (r *transactionRepository) RecordPINReveal(ctx context.Context, entry audit.Entry) error {
	err := database.WithinTx(ctx, r.db, func(tx database.Tx) error {
		return r.recorder.Record(ctx, tx, entry)
	})
	if err != nil {
		r.logger.Error("record PIN reveal",
			zap.Uint("admin_id", entry.Actor.ID),
			zap.String("target_id", entry.TargetID),
			zap.Any("payload", entry.Payload),
			zap.Error(err),
		)
		return fmt.Errorf("record pin reveal %q: %w", entry.TargetID, err)
	}
	return nil
}
