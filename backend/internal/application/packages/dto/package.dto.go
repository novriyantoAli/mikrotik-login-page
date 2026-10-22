// Package dto holds the only types that cross the HTTP boundary for the Package
// catalog. The shape is frozen by the Login Page's client seam (PRD point 8):
// the page renders these strings verbatim, so they are display values, not the
// typed money/duration the entity stores.
package dto

// PackageResponse is one entry of GET /packages. Field names stay in the
// frozen Indonesian contract; renaming one breaks the Login Page.
type PackageResponse struct {
	ID           uint   `json:"id"`
	Name         string `json:"nama"`
	Price        string `json:"harga"`
	ActivePeriod string `json:"durasiActivePeriod"`
	SpeedLimit   string `json:"batasKecepatan"`
}
