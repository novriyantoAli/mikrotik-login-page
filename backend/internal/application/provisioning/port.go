// Package provisioning owns the port that writes hotspot accounts into
// FreeRADIUS. FreeRADIUS remains the source of truth for credentials and Active
// Period (ADR 0001); this port only says what must be true there.
package provisioning

import (
	"context"
	"time"
)

// Account is the desired state of one hotspot account: Phone Number as the
// RADIUS username, the current PIN as the password.
type Account struct {
	PhoneNumber string
	PIN         string
	Expiration  time.Time
	RateLimit   string
}

// Writer applies an Account to FreeRADIUS idempotently: applying the same
// Account twice must not double the Active Period.
type Writer interface {
	Apply(ctx context.Context, account Account) error
}
