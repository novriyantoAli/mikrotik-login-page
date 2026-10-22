// Package phone owns the one canonical form of an Indonesian Phone Number.
//
// Every Phone Number the backend stores, looks up in FreeRADIUS or sends an SMS
// to goes through Normalize first (PRD point 9), so a user who types "0812-3456
// 789", "+62 812 3456 789" or "628123456789" is the same account everywhere.
// Nothing else may implement its own variant of this rule.
package phone

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalid is returned (wrapped) when an input cannot be a Phone Number.
var ErrInvalid = errors.New("invalid phone number")

// Length bounds of a canonical number, in digits and including the "62"
// country code. Indonesian mobile numbers are 10-15 digits in international
// form; the bounds reject obvious typos without guessing a missing digit.
const (
	minDigits = 10
	maxDigits = 15
)

// Normalize returns the canonical form of a Phone Number: "62" followed by the
// national significant number, digits only, with no leading zero, spaces,
// hyphens or plus sign. Accepted inputs are the forms people actually type:
// "08…", "628…" and "+62 8…" (any spacing or separators).
func Normalize(input string) (string, error) {
	var digits strings.Builder
	for _, char := range input {
		switch char {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			digits.WriteRune(char)
		case '+', ' ', '\t', '-', '.', '(', ')':
			// Formatting only; dropped.
		default:
			return "", fmt.Errorf("%w: unexpected character", ErrInvalid)
		}
	}

	normalized := digits.String()
	switch {
	case strings.HasPrefix(normalized, "0"):
		// 08… is the national form; 62 replaces the trunk prefix.
		normalized = "62" + strings.TrimPrefix(normalized, "0")
	case strings.HasPrefix(normalized, "62"):
		// Already international.
	default:
		return "", fmt.Errorf("%w: missing 0 or 62 prefix", ErrInvalid)
	}

	// Phone Number is a mobile identity: the backend sends it SMS and uses it as
	// the hotspot username, so a landline is not a valid value here.
	if !strings.HasPrefix(normalized, "628") {
		return "", fmt.Errorf("%w: not a mobile number", ErrInvalid)
	}
	if len(normalized) < minDigits || len(normalized) > maxDigits {
		return "", fmt.Errorf("%w: must be %d-%d digits", ErrInvalid, minDigits, maxDigits)
	}
	return normalized, nil
}
