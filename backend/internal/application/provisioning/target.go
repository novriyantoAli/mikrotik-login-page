package provisioning

import "time"

// Target is the one rule for where an Active Period ends after a purchase
// (PRD point 21): an Active Period still running at now is extended, so its end
// moves by duration; an absent or finished one starts at base, which is the
// settlement time when provisioning and the current time when previewing an
// Extend (ADR 0017).
//
// Both the provisioner and the Extend preview call this function instead of
// repeating the arithmetic, so the expiration the buyer confirms is the
// expiration provisioning will apply.
func Target(current Expiration, now, base time.Time, duration time.Duration) time.Time {
	if current.Found && current.ExpiresAt.After(now) {
		return current.ExpiresAt.Add(duration)
	}
	return base.Add(duration)
}
