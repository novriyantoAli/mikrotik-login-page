package repository

import "testing"

// TestAuditPageBounds keeps the page numbers inside the range the reader promises.
// The bounds belong with the SQL rather than with one caller's politeness: a zero
// PageSize would otherwise mean "no LIMIT" and hand back the whole log.
func TestAuditPageBounds(t *testing.T) {
	cases := map[string]struct {
		page, pageSize     int
		wantPage, wantSize int
	}{
		"absent defaults to the first page":         {0, 0, 1, DefaultAuditPageSize},
		"a negative page is the first page":         {-3, 20, 1, 20},
		"a negative size is the default size":       {2, -1, 2, DefaultAuditPageSize},
		"an oversized page is capped":               {1, 5000, 1, MaxAuditPageSize},
		"an accepted page passes through untouched": {7, 50, 7, 50},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			page, pageSize := auditPageBounds(testCase.page, testCase.pageSize)
			if page != testCase.wantPage || pageSize != testCase.wantSize {
				t.Fatalf("bounds = %d/%d, want %d/%d",
					page, pageSize, testCase.wantPage, testCase.wantSize)
			}
		})
	}
}
