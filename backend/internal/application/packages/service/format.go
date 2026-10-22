package service

import (
	"fmt"
	"strconv"
	"strings"
)

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
