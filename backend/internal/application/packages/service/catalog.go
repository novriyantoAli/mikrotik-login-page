package service

import (
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
)

// starterCatalog is the deployment seed: the Packages a hotspot starts selling
// before the Admin Panel exists (ADR 0012). Codes are the stable identity, so
// they must never change; names, prices, durations and rate limits are only
// defaults and are not applied to a row that already exists.
var starterCatalog = []struct {
	code            string
	name            string
	priceIDR        int64
	durationMinutes int
	rateLimit       string
}{
	{code: "harian", name: "Paket Harian", priceIDR: 5000, durationMinutes: 24 * 60, rateLimit: "5M/5M"},
	{code: "tiga-hari", name: "Paket 3 Hari", priceIDR: 12000, durationMinutes: 3 * 24 * 60, rateLimit: "5M/5M"},
	{code: "mingguan", name: "Paket Mingguan", priceIDR: 25000, durationMinutes: 7 * 24 * 60, rateLimit: "10M/10M"},
	{code: "bulanan", name: "Paket Bulanan", priceIDR: 90000, durationMinutes: 30 * 24 * 60, rateLimit: "10M/10M"},
}

// StarterCatalog returns the seedable Packages, one entity per deployment
// default. Each call returns a fresh slice, so no caller can mutate the seed.
func StarterCatalog() []entity.Package {
	packages := make([]entity.Package, 0, len(starterCatalog))
	for _, row := range starterCatalog {
		packages = append(packages, entity.Package{
			Code:            row.code,
			Name:            row.name,
			PriceIDR:        row.priceIDR,
			DurationMinutes: row.durationMinutes,
			RateLimit:       row.rateLimit,
		})
	}
	return packages
}
