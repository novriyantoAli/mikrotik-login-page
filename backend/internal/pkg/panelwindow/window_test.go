package panelwindow_test

import (
	"errors"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/panelwindow"
)

// TestOfIsHalfOpenInTheDeploymentDay is the rule both panel lists depend on: a day
// is the WIB day, and `to` stops at the next day's first instant.
func TestOfIsHalfOpenInTheDeploymentDay(t *testing.T) {
	window, err := panelwindow.Of("2026-10-23", "2026-10-24")
	if err != nil {
		t.Fatalf("Of: %v", err)
	}

	wib := panelwindow.Location()
	if window.From == nil || !window.From.Equal(time.Date(2026, time.October, 23, 0, 0, 0, 0, wib)) {
		t.Fatalf("from = %v, want the first instant of 23 October WIB", window.From)
	}
	if window.To == nil || !window.To.Equal(time.Date(2026, time.October, 25, 0, 0, 0, 0, wib)) {
		t.Fatalf("to = %v, want the first instant after 24 October WIB", window.To)
	}
}

// TestOfLeavesAbsentBoundsOpen keeps "one side only" representable.
func TestOfLeavesAbsentBoundsOpen(t *testing.T) {
	window, err := panelwindow.Of("2026-10-23", "")
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	if window.From == nil || window.To != nil {
		t.Fatalf("window = %+v, want only a from bound", window)
	}
}

// TestOfRefusesAMalformedDay is the difference between a typo and an empty page.
func TestOfRefusesAMalformedDay(t *testing.T) {
	for name, raw := range map[string]string{
		"dashed the other way": "23-10-2026",
		"a full instant":       "2026-10-24T08:00:00Z",
		"not a day at all":     "kemarin",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := panelwindow.Of(raw, "")
			var dateError *panelwindow.DateError
			if !errors.As(err, &dateError) || dateError.Field != "from" {
				t.Fatalf("error = %v, want a from bound reason", err)
			}
		})
	}
}

// TestOfRefusesAReversedRange keeps a backwards range from answering an empty page.
func TestOfRefusesAReversedRange(t *testing.T) {
	_, err := panelwindow.Of("2026-10-25", "2026-10-24")

	var dateError *panelwindow.DateError
	if !errors.As(err, &dateError) || dateError.Field != "from" {
		t.Fatalf("error = %v, want a reversed-range reason", err)
	}
}
