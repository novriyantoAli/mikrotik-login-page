// Package dto holds the only types that cross the HTTP boundary for the
// passwordless entry path.
package dto

// RequestMagicLinkRequest asks for a Magic Link. PackageID is present when the
// link should open a checkout for that Package, absent when it should open the
// Account Status page. ReturnURL is optional and is only honored when it matches
// the deployment's Login Page allowlist (ADR 0011).
type RequestMagicLinkRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	PackageID   *uint  `json:"package_id"`
	ReturnURL   string `json:"return_url"`
}

// MagicLinkSentResponse is the only answer the request endpoint ever gives. It
// is identical whether or not that Phone Number has ever transacted, so the
// endpoint cannot be used to enumerate customers (PRD point 10).
type MagicLinkSentResponse struct {
	Sent bool `json:"sent"`
}

// ExchangeMagicLinkRequest carries the token from the SMS.
type ExchangeMagicLinkRequest struct {
	Token string `json:"token" binding:"required"`
}

// SessionResponse is the authenticated identity. It is what GET /auth/session
// answers and what every user endpoint will authorize against.
type SessionResponse struct {
	PhoneNumber string `json:"phone_number"`
	ExpiresAt   string `json:"expires_at"`
}

// ExchangeMagicLinkResponse is the session plus the intent stored with the link,
// so the app can land the user on the page they asked for.
type ExchangeMagicLinkResponse struct {
	SessionResponse
	// SessionToken is the raw cookie value. It is `json:"-"` on purpose: it only
	// ever travels in the Set-Cookie header, never in a response body.
	SessionToken string `json:"-"`
	Purpose      string `json:"purpose"`
	PackageID    *uint  `json:"package_id,omitempty"`
	ReturnURL    string `json:"return_url,omitempty"`
}
