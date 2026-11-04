// This file holds the catalog's field rules. They live in the service, not only in
// the panel's form, because the API is reachable directly and every value here ends
// up in a bill or in FreeRADIUS verbatim (ADR 0025 §6).
package service

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The bounds ADR 0025 §6 settled, exported so tests and the panel's copy cite the
// same numbers instead of restating a literal.
const (
	// MaxPackageNameLength is the longest Package name, after trimming.
	MaxPackageNameLength = 80
	// MaxPackagePriceIDR is the highest price, so a slipped zero is caught.
	MaxPackagePriceIDR = 100_000_000
	// MaxPackageDurationMinutes is 366 days: no Package is longer than a year.
	MaxPackageDurationMinutes = 527_040
)

// codePattern is the seed's identity format. A code outside it would be a row the
// seed can never match, which is why the format is enforced rather than trimmed.
var codePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// rateLimitPattern is the MikroTik-Rate-Limit shape written to FreeRADIUS
// unchanged: "download/upload", each side a number with an optional k/M/G suffix.
var rateLimitPattern = regexp.MustCompile(`^\d+(\.\d+)?[kKmMgG]?/\d+(\.\d+)?[kKmMgG]?$`)

// FieldError names one refused field and why, in the words the panel shows. It
// implements Is so every field rule reads as ErrPackageInvalid while the message
// still tells the operator what to fix.
type FieldError struct {
	Field  string
	Reason string
}

// Error is the operator-facing reason, never a Go value dump.
func (e *FieldError) Error() string { return e.Reason }

// Is makes errors.Is(err, ErrPackageInvalid) true for every field rule.
func (e *FieldError) Is(target error) bool { return target == ErrPackageInvalid }

func validateCode(code string) error {
	if !codePattern.MatchString(code) {
		return &FieldError{
			Field:  "code",
			Reason: "Code paket hanya boleh huruf kecil, angka, dan tanda hubung (contoh: paket-harian).",
		}
	}
	return nil
}

// validateName trims and checks the name, returning the value to store so a caller
// cannot accidentally save the untrimmed input.
func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", &FieldError{Field: "name", Reason: "Nama paket wajib diisi."}
	}
	if utf8.RuneCountInString(name) > MaxPackageNameLength {
		return "", &FieldError{
			Field:  "name",
			Reason: fmt.Sprintf("Nama paket paling banyak %d karakter.", MaxPackageNameLength),
		}
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", &FieldError{Field: "name", Reason: "Nama paket tidak boleh memuat karakter kontrol."}
		}
	}
	return name, nil
}

func validatePrice(priceIDR int64) error {
	if priceIDR <= 0 || priceIDR > MaxPackagePriceIDR {
		return &FieldError{
			Field: "price_idr",
			Reason: fmt.Sprintf("Harga harus bilangan bulat lebih dari 0 dan paling banyak Rp %s.",
				FormatRupiah(MaxPackagePriceIDR)),
		}
	}
	return nil
}

func validateDuration(durationMinutes int) error {
	if durationMinutes <= 0 || durationMinutes > MaxPackageDurationMinutes {
		return &FieldError{
			Field:  "duration_minutes",
			Reason: fmt.Sprintf("Durasi harus lebih dari 0 dan paling banyak %d menit (366 hari).", MaxPackageDurationMinutes),
		}
	}
	return nil
}

func validateRateLimit(rateLimit string) error {
	if !rateLimitPattern.MatchString(rateLimit) {
		return &FieldError{Field: "rate_limit", Reason: "Batas kecepatan harus berbentuk unduh/unggah seperti 5M/5M."}
	}
	return nil
}
