// Package entity holds the persisted shape of the auth domain: Magic Link tokens
// and sessions. Both store only a hash of the value the user holds, never the
// value itself (ADR 0011).
package entity

import "time"

// Purpose is what a Magic Link opens: a checkout for one Package, or the Account
// Status page.
type Purpose string

// Magic Link purposes. The strings travel to the app, so keep them stable.
const (
	PurposeCheckout Purpose = "checkout"
	PurposeAccount  Purpose = "account"
)

// MagicLinkToken is one issued link. UsedAt and ExpiresAt are the only guards a
// redemption has to satisfy.
type MagicLinkToken struct {
	ID uint `gorm:"primaryKey"`

	// TokenHash is the SHA-256 of the token in the SMS; the raw token is never
	// stored (ADR 0011).
	TokenHash string `gorm:"column:token_hash;not null;uniqueIndex"`
	// PhoneNumber is canonical ("628…") and is the only client identity.
	PhoneNumber string `gorm:"column:phone_number;not null;index"`

	Purpose   Purpose `gorm:"column:purpose;not null"`
	PackageID *uint   `gorm:"column:package_id"`
	// ReturnURL is already validated against the deployment allowlist when the
	// link is issued, so redeeming never sees an unchecked redirect.
	ReturnURL string `gorm:"column:return_url;not null;default:''"`

	ExpiresAt time.Time  `gorm:"column:expires_at;not null"`
	UsedAt    *time.Time `gorm:"column:used_at"`

	CreatedAt time.Time
}

// TableName pins the plural snake_case table name.
func (MagicLinkToken) TableName() string { return "magic_link_tokens" }

// Session is one browser session. RevokedAt makes sign-out immediate: the cookie
// may still arrive, the lookup simply stops matching (ADR 0009/0011).
type Session struct {
	ID uint `gorm:"primaryKey"`

	TokenHash   string     `gorm:"column:token_hash;not null;uniqueIndex"`
	PhoneNumber string     `gorm:"column:phone_number;not null;index"`
	ExpiresAt   time.Time  `gorm:"column:expires_at;not null"`
	RevokedAt   *time.Time `gorm:"column:revoked_at"`

	CreatedAt time.Time
}

// TableName pins the plural snake_case table name.
func (Session) TableName() string { return "sessions" }
