package repository_test

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	adminrepository "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/admin/repository"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/audit"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// TestAuditRecorderRefusesTheZeroTransaction is the unit half of "one row, one
// transaction": a recorder asked to write without a live handle answers its own
// sentinel instead of appending a row that could commit on its own (ADR 0027 §2).
func TestAuditRecorderRefusesTheZeroTransaction(t *testing.T) {
	t.Parallel()

	recorder := adminrepository.NewAuditRecorder(zap.NewNop())
	err := recorder.Record(context.Background(), database.Tx{}, audit.Entry{
		Actor:  audit.Actor{ID: 1, Email: "owner@example.test"},
		Action: audit.ActionAdminCreate,
	})
	if !errors.Is(err, adminrepository.ErrAuditWithoutTransaction) {
		t.Fatalf("error = %v, want ErrAuditWithoutTransaction", err)
	}
}
