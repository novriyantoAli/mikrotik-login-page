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
type Package struct {
	ID              uint   `gorm:"primaryKey"`
	Code            string `gorm:"column:code;not null;uniqueIndex"`
	Name            string `gorm:"column:name;not null"`
	PriceIDR        int64  `gorm:"column:price_idr;not null"`
	DurationMinutes int    `gorm:"column:duration_minutes;not null"`
	RateLimit       string `gorm:"column:rate_limit;not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName pins the plural snake_case table name.
func (Package) TableName() string { return "packages" }
