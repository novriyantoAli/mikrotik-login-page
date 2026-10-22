// Package clock makes time a dependency instead of a global. Expiry, session
// lifetime and Magic Link validity must be testable without waiting, so nothing
// in domain code calls time.Now directly.
package clock

import "time"

// Clock is the narrow surface domain code may use.
type Clock interface {
	Now() time.Time
}

// System reads the wall clock.
type System struct{}

// New returns the production clock.
func New() Clock { return System{} }

// Now implements Clock.
func (System) Now() time.Time { return time.Now().UTC() }
