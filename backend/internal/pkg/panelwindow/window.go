// Package panelwindow parses the Admin Panel's day-range filters. Both panel
// lists (the Transaction search and the audit reader) ask for "23 October", not for
// an instant, and both mean the deployment's own day (WIB) — the same zone the panel
// renders timestamps in, so a filter and the column an operator just read can never
// disagree by seven hours.
//
// It lives here rather than in either domain because the window is one rule with one
// shape; two copies would drift the day one of them learns about a new bound. The
// operator-facing wording travels with the error, so both readers refuse a typo the
// same way.
package panelwindow

import "time"

// Layout is the day format the panel's date inputs send.
const Layout = time.DateOnly

// dayLocation is the day boundary a search means: WIB, fixed, because the deployment
// has one time zone and the panel's columns render in it.
var dayLocation = time.FixedZone("WIB", 7*60*60)

// Window is a half-open time range: From is inclusive, To is exclusive, so two
// adjacent days never both match the same row and a day is always exactly 24 hours.
// A nil side means "no bound on this side".
type Window struct {
	From *time.Time
	To   *time.Time
}

// DateError names the refused bound and why, in the words the panel shows. Each
// domain maps it onto its own filter error so callers keep branching on one sentinel
// per search.
type DateError struct {
	Field  string
	Reason string
}

// Error is the operator-facing reason, never a Go value dump.
func (e *DateError) Error() string { return e.Reason }

// Of parses the two day bounds. The input names a day the operator wants included,
// so `to` stops at the first instant of the day after it. An absent bound is nil.
func Of(rawFrom, rawTo string) (Window, error) {
	from, err := day(rawFrom, "from")
	if err != nil {
		return Window{}, err
	}

	to, err := day(rawTo, "to")
	if err != nil {
		return Window{}, err
	}
	if to == nil {
		return Window{From: from}, nil
	}

	end := to.AddDate(0, 0, 1)
	if from != nil && !end.After(*from) {
		return Window{}, &DateError{
			Field:  "from",
			Reason: "Rentang tanggal terbalik: isi tanggal awal sebelum tanggal akhir.",
		}
	}
	return Window{From: from, To: &end}, nil
}

// day parses one optional bound.
func day(raw string, field string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}

	parsed, err := time.ParseInLocation(Layout, raw, dayLocation)
	if err != nil {
		return nil, &DateError{
			Field:  field,
			Reason: "Tanggal harus berbentuk YYYY-MM-DD.",
		}
	}
	return &parsed, nil
}

// Location returns the zone a day is parsed in. It is exported for tests that pin
// the seven-hour boundary the panel depends on.
func Location() *time.Location { return dayLocation }
