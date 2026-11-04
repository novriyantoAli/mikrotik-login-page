// Audit adapter is the one place that writes the append-only trail. It lives in
// the admin domain because the table is the admin domain's (ADR 0027 §1), but it
// is deliberately transport-free and dependency-free: the domain that records an
// action passes the caller's transaction in, so the trail row and the change it
// witnesses commit or roll back together (ADR 0027 §2).
package repository

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// ErrAuditWithoutTransaction means a caller asked for a trail row without handing
// in a transaction. It is a refusal, not a warning: an entry that commits on its
// own can survive a rolled-back change, which is exactly the "change with no
// witness, or witness with no change" pair the trail must never contain.
//
// The check is a nil handle, not a runtime type assertion: database.WithinTx is
// the only way to build a non-zero database.Tx, so a handle that exists is a
// transaction (ADR 0027 §2).
var ErrAuditWithoutTransaction = errors.New("audit entry needs the caller's transaction")

// AuditRecorder appends one row per privileged action. It satisfies audit.Recorder,
// the one interface every recording domain depends on, and is bound to it by the
// composition root, so nothing in the graph imports admin to record.
type AuditRecorder struct {
	logger *zap.Logger
}

// NewAuditRecorder builds the adapter. It holds no database handle on purpose:
// the only handle it may write through is the one its caller passes in.
func NewAuditRecorder(logger *zap.Logger) *AuditRecorder {
	return &AuditRecorder{logger: logger}
}

// Record appends exactly one trail row inside the caller's transaction.
func (r *AuditRecorder) Record(ctx context.Context, tx database.Tx, entry audit.Entry) error {
	handle := tx.DB()
	if handle == nil {
		return ErrAuditWithoutTransaction
	}

	row := rowFromAudit(entry)
	if err := handle.WithContext(ctx).Create(row).Error; err != nil {
		r.logger.Error("append audit row",
			zap.String("action", string(entry.Action)),
			zap.String("target_type", string(entry.TargetType)),
			zap.String("target_id", entry.TargetID),
			zap.Error(err),
		)
		return fmt.Errorf("append audit row %q: %w", entry.Action, err)
	}
	return nil
}

// rowFromAudit maps the shared fact onto the persisted row. The actor's email and
// id are copied, never joined, so the row still reads after the admin is gone.
func rowFromAudit(entry audit.Entry) *entity.AuditEntry {
	payload := entity.Payload(entry.Payload)
	if payload == nil {
		payload = entity.Payload{}
	}

	return &entity.AuditEntry{
		AdminID:    entry.Actor.ID,
		AdminEmail: entry.Actor.Email,
		Action:     entry.Action,
		TargetType: string(entry.TargetType),
		TargetID:   entry.TargetID,
		Payload:    payload,
		IP:         entry.Actor.IP,
	}
}
