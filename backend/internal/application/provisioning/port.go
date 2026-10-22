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

// Expiration is the Active Period end FreeRADIUS holds for one Phone Number.
// Found is false when the account has no expiration at all, which the public
// Active Period read reports as an empty state instead of an error (ADR 0014).
type Expiration struct {
	ExpiresAt time.Time
	Found     bool
}

// ExpirationReader reads the Active Period end for a Phone Number.
//
// It is a port on purpose: only the MySQL adapter knows which FreeRADIUS table
// and attribute hold it, so the Login Page contract can be tested without a
// MySQL server (ADR 0014). phoneNumber is always canonical ("628…").
type ExpirationReader interface {
	Expiration(ctx context.Context, phoneNumber string) (Expiration, error)
}
