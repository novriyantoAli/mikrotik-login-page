// Package dto holds the only types that cross the HTTP boundary for the public
// Active Period endpoint. The shape is frozen by the Login Page (ADR 0005):
// {"data":{"expiry":<ISO-8601 or null>}}.
package dto

// ActivePeriodResponse is the body of GET /active-period. Expiry is nil when
// the Phone Number has no Active Period at all, which the status page renders
// as an empty state rather than as a failure.
type ActivePeriodResponse struct {
	Expiry *string `json:"expiry"`
}
