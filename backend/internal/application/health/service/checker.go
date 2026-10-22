package service

import "context"

// CheckersGroup is the Fx group every dependency plugs into. Infrastructure
// packages provide checkers; this domain never imports the database drivers.
const CheckersGroup = "health_checkers"

// Checker probes one dependency. A checker must respect the context deadline
// and return a non-nil error when the dependency is unusable.
type Checker interface {
	// Name is the key this dependency appears under in the response body.
	Name() string
	// Check reports whether the dependency is reachable right now.
	Check(ctx context.Context) error
}
