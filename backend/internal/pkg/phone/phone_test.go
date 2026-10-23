package phone_test

import (
	"errors"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/phone"
)

func TestNormalizeAcceptsTheFormsPeopleType(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"08123456789":        "628123456789",
		"0812 3456 789":      "628123456789",
		"0812-3456-789":      "628123456789",
		"0812.3456.789":      "628123456789",
		"+628123456789":      "628123456789",
		"+62 812 3456 789":   "628123456789",
		"62 812 3456 789":    "628123456789",
		"628123456789":       "628123456789",
		"(0812) 3456-789":    "628123456789",
		"  08123456789  ":    "628123456789",
		"+62 (812) 3456-789": "628123456789",
	}

	for input, want := range cases {
		got, err := phone.Normalize(input)
		if err != nil {
			t.Errorf("Normalize(%q) error = %v, want %q", input, err, want)
			continue
		}
		if got != want {
			t.Errorf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeIsIdempotent(t *testing.T) {
	t.Parallel()

	once, err := phone.Normalize("0812-3456-789")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	twice, err := phone.Normalize(once)
	if err != nil {
		t.Fatalf("Normalize(canonical): %v", err)
	}
	if once != twice {
		t.Fatalf("Normalize is not idempotent: %q != %q", once, twice)
	}
}

func TestNormalizeRejectsValuesThatAreNotPhoneNumbers(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"",
		"   ",
		"0812",              // too short
		"6281234",           // too short
		"12345",             // no 0/62 prefix
		"8123456789",        // missing the trunk prefix entirely
		"0211234567",        // landline, cannot receive SMS
		"62812345678901234", // too long
		"0812-ABCD-789",
		"0812;3456;789",
		"+1 415 555 0100", // not Indonesian
	} {
		if _, err := phone.Normalize(input); !errors.Is(err, phone.ErrInvalid) {
			t.Errorf("Normalize(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestMaskKeepsEndsOnly(t *testing.T) {
	if got := phone.Mask("6281234567890"); got != "6281****7890" {
		t.Fatalf("Mask = %q", got)
	}
	if got := phone.Mask("62812"); got != "****" {
		t.Fatalf("Mask of a short value = %q, want fully masked", got)
	}
}
