package database_test

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// TestRadiusWriter_RejectsIncompleteAccounts proves the guard runs before any
// database work: a service bug must be an error, not a panic on a nil handle or
// an account FreeRADIUS silently cannot use.
func TestRadiusWriter_RejectsIncompleteAccounts(t *testing.T) {
	t.Parallel()

	writer := database.NewRadiusWriter(&database.FreeRADIUS{}, zap.NewNop())
	expiration := time.Date(2026, time.October, 24, 8, 0, 0, 0, time.UTC)

	cases := map[string]provisioning.Account{
		"no phone number": {PIN: "123456", Expiration: expiration},
		"no pin":          {PhoneNumber: "6281200000001", Expiration: expiration},
		"no expiration":   {PhoneNumber: "6281200000001", PIN: "123456"},
		"before epoch": {
			PhoneNumber: "6281200000001",
			PIN:         "123456",
			Expiration:  time.Unix(-1, 0),
		},
	}

	for name, account := range cases {
		if err := writer.Apply(context.Background(), account); err == nil {
			t.Fatalf("%s: Apply accepted an incomplete account", name)
		}
	}
}
