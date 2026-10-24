// Package dto holds the only types that cross the HTTP boundary for Account
// Status. The page is a read composition, not a stored account: these shapes are
// what the app renders and what the shared contract fixtures pin (ADR 0017).
package dto

// ActivePeriodState is how the app labels the Active Period. The backend computes
// it from account.expiry_warning_hours against the injected clock, so no client
// ever compares its own clock with an expiration (ADR 0017).
type ActivePeriodState string

const (
	// StateActive means the Active Period ends later than the warning window.
	StateActive ActivePeriodState = "active"
	// StateExpiring means it ends within the warning window but not yet.
	StateExpiring ActivePeriodState = "expiring"
	// StateExpired means the expiration has passed.
	StateExpired ActivePeriodState = "expired"
	// StateNone means FreeRADIUS holds no expiration at all, which is the "never
	// bought anything" state and is different from a finished one.
	StateNone ActivePeriodState = "none"
)

// ActivePeriodResponse is the summary's view of the Active Period. ExpiresAt is
// RFC 3339 when found, null otherwise; Found keeps "no row" explicit even before
// State is read.
type ActivePeriodResponse struct {
	ExpiresAt *string           `json:"expires_at"`
	Found     bool              `json:"found"`
	State     ActivePeriodState `json:"state"`
}

// PackageSummaryResponse is the Package the current account bought. RateLimit is
// the stored MikroTik-Rate-Limit value ("5M/5M"); the app shows the download side.
type PackageSummaryResponse struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	RateLimit string `json:"rate_limit"`
}

// AccountResponse is the whole Account Status summary for the session's Phone
// Number: the Active Period, the Package behind it, and the current PIN. It is
// the only read that carries a PIN, and the app renders that PIN hidden by
// default (ADR 0017).
type AccountResponse struct {
	PhoneNumber  string                  `json:"phone_number"`
	ActivePeriod ActivePeriodResponse    `json:"active_period"`
	Package      *PackageSummaryResponse `json:"package"`
	PIN          *string                 `json:"pin"`
}

// ExtendPreviewResponse is the estimate an Extend confirmation shows before the
// buyer pays. It is not the final value: provisioning recomputes it with the same
// rule at settlement time (ADR 0017). Extends is true when a running Active
// Period is being added to rather than restarted.
type ExtendPreviewResponse struct {
	PackageID           uint    `json:"package_id"`
	CurrentExpiration   *string `json:"current_expiration"`
	EstimatedExpiration string  `json:"estimated_expiration"`
	Extends             bool    `json:"extends"`
}
