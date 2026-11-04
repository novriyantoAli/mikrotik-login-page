// Package dto holds the only types that cross the HTTP boundary for the Package
// catalog. Two contracts live here: the public one, frozen by the Login Page's
// client seam (PRD point 8), where the page renders display strings verbatim; and
// the panel's, which carries the typed money/duration the edit form needs.
package dto

import "time"

// PackageResponse is one entry of GET /packages. Field names stay in the
// frozen Indonesian contract; renaming one breaks the Login Page.
type PackageResponse struct {
	ID           uint   `json:"id"`
	Name         string `json:"nama"`
	Price        string `json:"harga"`
	ActivePeriod string `json:"durasiActivePeriod"`
	SpeedLimit   string `json:"batasKecepatan"`
}

// AdminPackageResponse is one row of GET /api/admin/packages. It is deliberately
// not PackageResponse: the panel edits typed values (integer rupiah, minutes, the
// MikroTik rate limit) and must see inactive Packages and the row's version, none
// of which the frozen public contract carries (ADR 0025 §1).
type AdminPackageResponse struct {
	ID              uint      `json:"id" example:"1"`
	Code            string    `json:"code" example:"harian"`
	Name            string    `json:"name" example:"Paket Harian"`
	PriceIDR        int64     `json:"price_idr" example:"5000"`
	DurationMinutes int       `json:"duration_minutes" example:"1440"`
	RateLimit       string    `json:"rate_limit" example:"5M/5M"`
	IsActive        bool      `json:"is_active" example:"true"`
	Version         int       `json:"version" example:"1"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CreatePackageRequest is a new catalog row. Code is the seed's identity and is
// immutable afterwards, so it only ever appears here (ADR 0025 §5). The service
// re-checks every field: the panel's form is one caller, not the boundary.
type CreatePackageRequest struct {
	Code            string `json:"code" binding:"required" example:"harian"`
	Name            string `json:"name" binding:"required" example:"Paket Harian"`
	PriceIDR        int64  `json:"price_idr" example:"5000"`
	DurationMinutes int    `json:"duration_minutes" example:"1440"`
	RateLimit       string `json:"rate_limit" binding:"required" example:"5M/5M"`
}

// UpdatePackageRequest is an edit of a Package the panel already read. Version is
// required so a price the admin never saw cannot be overwritten; Code is accepted
// only so an attempt to change it is refused instead of silently ignored.
type UpdatePackageRequest struct {
	Name            string `json:"name" binding:"required" example:"Paket Harian"`
	PriceIDR        int64  `json:"price_idr" example:"5000"`
	DurationMinutes int    `json:"duration_minutes" example:"1440"`
	RateLimit       string `json:"rate_limit" binding:"required" example:"5M/5M"`
	Version         int    `json:"version" binding:"required" example:"1"`
	// Code is the current identity, echoed by the form. Any other value is a
	// refused edit, not a rename (ADR 0025 §5).
	Code string `json:"code" example:"harian"`
}

// SetActiveRequest retires or revives one Package. Active is a pointer because
// `false` is a value this body must be able to carry, and `binding:"required"`
// rejects a zero-valued bool.
type SetActiveRequest struct {
	Active *bool `json:"active" binding:"required" example:"false"`
}

// VersionConflictResponse is the 409 body of a stale Package edit: the shared
// error envelope plus the row as it is now, so the panel can show what changed
// instead of re-reading blind (ADR 0025 §7). The extra field is confined to this
// one endpoint rather than widening the envelope every failure uses.
type VersionConflictResponse struct {
	Error VersionConflictBody `json:"error"`
}

// VersionConflictBody is the error object of a stale edit.
type VersionConflictBody struct {
	Code    string               `json:"code" example:"conflict"`
	Message string               `json:"message"`
	Current AdminPackageResponse `json:"current"`
}
