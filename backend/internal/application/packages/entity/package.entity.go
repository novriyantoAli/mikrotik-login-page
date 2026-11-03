// Package entity holds the persisted shape of a Package: the sellable product
// the Login Page lists and the user buys an Active Period of. Entities carry
// gorm tags only; the JSON contract lives in dto (golden rule 6).
package entity

import "time"

// Package is one catalog row. PriceIDR is integer rupiah (no floats anywhere),
// DurationMinutes is the Active Period a purchase grants, and RateLimit is the
// MikroTik-Rate-Limit value written to FreeRADIUS ("download/upload", e.g.
// "5M/5M"). Code is the stable identity the deployment seed keys on, so
// re-seeding never duplicates a Package (ADR 0012); it is not part of the
// public Login Page contract.
//
// A Package is retired, never deleted (ADR 0025): IsActive false keeps the row,
// its Code and its history while removing it from the public catalog. Version is
// the optimistic-concurrency token every write must present, so a price edit
// cannot silently overwrite one the admin never saw.
type Package struct {
	ID              uint   `gorm:"primaryKey"`
	Code            string `gorm:"column:code;not null;uniqueIndex"`
	Name            string `gorm:"column:name;not null"`
	PriceIDR        int64  `gorm:"column:price_idr;not null"`
	DurationMinutes int    `gorm:"column:duration_minutes;not null"`
	RateLimit       string `gorm:"column:rate_limit;not null"`
	// IsActive is whether the Package is still sold. Once the write endpoints and
	// the admin list land, the public catalog filters on it while the panel shows
	// both states (ADR 0025 §1). Retiring a Package is always written with an
	// explicit column update: `false` is a zero value GORM drops on insert.
	IsActive bool `gorm:"column:is_active;not null;default:true"`
	// Version starts at 1 and is incremented on every write. It is an integer
	// rather than UpdatedAt because two writes can share a timestamp (ADR 0025 §7).
	Version int `gorm:"column:version;not null;default:1"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName pins the plural snake_case table name.
func (Package) TableName() string { return "packages" }
