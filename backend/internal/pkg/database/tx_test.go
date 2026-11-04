package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// TestWithinTxRefusesToRunWithoutAHandle keeps the failure a programming error
// with a name, instead of a nil-pointer panic inside GORM.
func TestWithinTxRefusesToRunWithoutAHandle(t *testing.T) {
	t.Parallel()

	called := false
	err := database.WithinTx(context.Background(), nil, func(database.Tx) error {
		called = true
		return nil
	})
	if !errors.Is(err, database.ErrNoTransaction) {
		t.Fatalf("error = %v, want ErrNoTransaction", err)
	}
	if called {
		t.Fatal("WithinTx ran the callback without a handle")
	}
}

// TestZeroTxMeansNoTransaction is the property the audit adapter relies on: the
// zero value is not a usable transaction, and only WithinTx can build one.
func TestZeroTxMeansNoTransaction(t *testing.T) {
	t.Parallel()

	var tx database.Tx
	if tx.DB() != nil {
		t.Fatal("the zero Tx carries a database handle; only WithinTx may build one")
	}
}
