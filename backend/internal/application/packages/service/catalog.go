package service

import (
	"fmt"
	"strconv"
	"strings"

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

// FormatRupiah renders integer rupiah the way the Login Page shows it
// ("Rp 5.000"). Money stays an integer everywhere else; this is presentation.
func FormatRupiah(amountIDR int64) string {
	sign := ""
	digits := strconv.FormatInt(amountIDR, 10)
	if amountIDR < 0 {
		sign = "-"
		digits = strconv.FormatInt(-amountIDR, 10)
	}

	var grouped strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(digit)
	}
	return sign + "Rp " + grouped.String()
}

// FormatActivePeriod renders an Active Period length in the largest whole unit
// ("1 Hari", "12 Jam", "45 Menit").
func FormatActivePeriod(minutes int) string {
	if minutes <= 0 {
		return "0 Menit"
	}
	switch {
	case minutes%(24*60) == 0:
		return fmt.Sprintf("%d Hari", minutes/(24*60))
	case minutes%60 == 0:
		return fmt.Sprintf("%d Jam", minutes/60)
	default:
		return fmt.Sprintf("%d Menit", minutes)
	}
}

// FormatSpeedLimit renders the download side of a MikroTik-Rate-Limit value:
// "5M/5M" is 5 Mbps down and up, so the catalog shows "5M".
func FormatSpeedLimit(rateLimit string) string {
	download, _, _ := strings.Cut(rateLimit, "/")
	return strings.TrimSpace(download)
}
