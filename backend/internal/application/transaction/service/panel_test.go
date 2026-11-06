package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	notificationservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
)

// panelRow is one Transaction as the panel's list will show it: a settled
// purchase, its snapshot, and the attempts it cost.
func panelRow(id uint, reference, phoneNumber string, packageID uint, state entity.State, at time.Time) *entity.Transaction {
	return &entity.Transaction{
		ID:                id,
		Reference:         reference,
		GatewayReference:  "GW-" + reference,
		PhoneNumber:       phoneNumber,
		PackageID:         packageID,
		PackageName:       "Paket Harian",
		AmountIDR:         5000,
		DurationMinutes:   1440,
		RateLimit:         "5M/5M",
		Channel:           "BRIVA",
		ChannelName:       "BRIVA",
		State:             state,
		ExpiresAt:         at.Add(time.Hour),
		ProvisionAttempts: 2,
		CreatedAt:         at,
	}
}

func TestListTransactions_SearchesByStatePhonePackageAndDay(t *testing.T) {
	t.Parallel()

	f := newFixture()
	first := panelRow(1, "TRX-ONE", testPhone, 7, entity.StateProvisioned,
		time.Date(2026, time.October, 22, 23, 59, 0, 0, time.UTC))
	second := panelRow(2, "TRX-TWO", otherPhone, 8, entity.StatePaid,
		time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC))
	third := panelRow(3, "TRX-THREE", otherPhone, 7, entity.StateExpired,
		time.Date(2026, time.October, 24, 0, 0, 0, 0, time.UTC))
	f.repo.rows = append(f.repo.rows, *first, *second, *third)

	cases := []struct {
		name   string
		filter dto.AdminTransactionFilter
		want   []uint
	}{
		{
			name:   "no filter returns everything newest first",
			filter: dto.AdminTransactionFilter{},
			want:   []uint{3, 2, 1},
		},
		{
			name:   "state",
			filter: dto.AdminTransactionFilter{State: string(entity.StatePaid)},
			want:   []uint{2},
		},
		{
			name:   "phone number typed the national way",
			filter: dto.AdminTransactionFilter{PhoneNumber: "081200000002"},
			want:   []uint{3, 2},
		},
		{
			name:   "package",
			filter: dto.AdminTransactionFilter{PackageID: 7},
			want:   []uint{3, 1},
		},
		{
			// The day is WIB, not UTC: `first` is 06:59 WIB on the 23rd and `second`
			// is 15:00 WIB on the same day, so one search returns both.
			name:   "one whole day, both bounds included",
			filter: dto.AdminTransactionFilter{From: "2026-10-23", To: "2026-10-23"},
			want:   []uint{2, 1},
		},
		{
			name:   "from a day onwards",
			filter: dto.AdminTransactionFilter{From: "2026-10-23"},
			want:   []uint{3, 2, 1},
		},
		{
			name:   "up to a day",
			filter: dto.AdminTransactionFilter{To: "2026-10-23"},
			want:   []uint{2, 1},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// Not parallel: the cases share one fake outbox, which records the
			// numbers it was asked about.
			page, err := f.service.ListTransactions(context.Background(), &testCase.filter)
			if err != nil {
				t.Fatalf("ListTransactions: %v", err)
			}

			got := make([]uint, 0, len(page.Items))
			for _, item := range page.Items {
				got = append(got, item.ID)
			}
			if len(got) != len(testCase.want) {
				t.Fatalf("ids = %v, want %v", got, testCase.want)
			}
			for index := range got {
				if got[index] != testCase.want[index] {
					t.Fatalf("ids = %v, want %v", got, testCase.want)
				}
			}
			if page.TotalCount != int64(len(testCase.want)) {
				t.Fatalf("total = %d, want %d", page.TotalCount, len(testCase.want))
			}
		})
	}
}

func TestListTransactions_RefusesAFilterItCannotAnswer(t *testing.T) {
	t.Parallel()

	f := newFixture()

	cases := []struct {
		name   string
		filter dto.AdminTransactionFilter
		field  string
	}{
		{name: "unknown state", filter: dto.AdminTransactionFilter{State: "shipped"}, field: "state"},
		{name: "not a phone number", filter: dto.AdminTransactionFilter{PhoneNumber: "not-a-number"}, field: "phone_number"},
		{name: "from is not a day", filter: dto.AdminTransactionFilter{From: "23-10-2026"}, field: "from"},
		{name: "to is not a day", filter: dto.AdminTransactionFilter{To: "kemarin"}, field: "to"},
		{
			name:   "range backwards",
			filter: dto.AdminTransactionFilter{From: "2026-10-24", To: "2026-10-23"},
			field:  "from",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := f.service.ListTransactions(context.Background(), &testCase.filter)
			if !errors.Is(err, service.ErrFilterInvalid) {
				t.Fatalf("error = %v, want ErrFilterInvalid", err)
			}

			var refused *service.FilterError
			if !errors.As(err, &refused) {
				t.Fatalf("error = %v, want the filter's own reason", err)
			}
			if refused.Field != testCase.field {
				t.Fatalf("refused field = %q, want %q", refused.Field, testCase.field)
			}
		})
	}
}

func TestListTransactions_CarriesTheLastSMSStatusAndNeverThePIN(t *testing.T) {
	t.Parallel()

	f := newFixture()
	row := panelRow(1, "TRX-SMS", testPhone, 7, entity.StateProvisioned,
		time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC))
	f.repo.rows = append(f.repo.rows, *row)
	f.smsStatus.statuses[testPhone] = notificationservice.DeliveryStatus{
		State:     "sent",
		UpdatedAt: time.Date(2026, time.October, 23, 8, 2, 0, 0, time.UTC),
	}

	page, err := f.service.ListTransactions(context.Background(), &dto.AdminTransactionFilter{})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}

	item := page.Items[0]
	if item.LastSMSState == nil || *item.LastSMSState != "sent" {
		t.Fatalf("last_sms_state = %v, want sent", item.LastSMSState)
	}
	if item.LastSMSAt == nil || *item.LastSMSAt != "2026-10-23T08:02:00Z" {
		t.Fatalf("last_sms_at = %v", item.LastSMSAt)
	}
	if item.ProvisionAttempts != row.ProvisionAttempts {
		t.Fatalf("provision_attempts = %d, want %d", item.ProvisionAttempts, row.ProvisionAttempts)
	}
	if item.PackageName != row.PackageName || item.AmountIDR != row.AmountIDR {
		t.Fatalf("row shows %q/%d, want the snapshot %q/%d",
			item.PackageName, item.AmountIDR, row.PackageName, row.AmountIDR)
	}
	// A page asks the outbox once, for its own rows.
	if len(f.smsStatus.asked) != 1 || f.smsStatus.asked[0] != testPhone {
		t.Fatalf("outbox was asked about %v, want exactly %q", f.smsStatus.asked, testPhone)
	}
}

func TestListTransactions_LeavesTheSMSStatusEmptyWhenNothingWasQueued(t *testing.T) {
	t.Parallel()

	f := newFixture()
	row := panelRow(1, "TRX-NOSMS", testPhone, 7, entity.StatePending,
		time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC))
	f.repo.rows = append(f.repo.rows, *row)

	page, err := f.service.ListTransactions(context.Background(), &dto.AdminTransactionFilter{})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}

	if page.Items[0].LastSMSState != nil {
		t.Fatalf("last_sms_state = %v, want nil for a Transaction with no message",
			*page.Items[0].LastSMSState)
	}
}

func TestListTransactions_ReportsABrokenOutboxInsteadOfInventingAStatus(t *testing.T) {
	t.Parallel()

	f := newFixture()
	row := panelRow(1, "TRX-ERR", testPhone, 7, entity.StateProvisioned,
		time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC))
	f.repo.rows = append(f.repo.rows, *row)
	f.smsStatus.err = errors.New("outbox is down")

	if _, err := f.service.ListTransactions(context.Background(), &dto.AdminTransactionFilter{}); err == nil {
		t.Fatal("a broken outbox read was silently swallowed")
	}
}

func TestListTransactions_BoundsThePageLikeEveryOtherList(t *testing.T) {
	t.Parallel()

	f := newFixture()
	for index := uint(1); index <= 3; index++ {
		row := panelRow(index, "TRX-PAGE", testPhone, 7, entity.StatePaid,
			time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC).Add(time.Duration(index)*time.Minute))
		f.repo.rows = append(f.repo.rows, *row)
	}

	page, err := f.service.ListTransactions(context.Background(), &dto.AdminTransactionFilter{
		Page:     2,
		PageSize: 2,
	})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(page.Items) != 1 || page.Page != 2 || page.PageSize != 2 || page.TotalCount != 3 {
		t.Fatalf("page = %d/%d of %d items, want 2/2 of 3 with one row",
			page.Page, page.PageSize, page.TotalCount)
	}

	// An oversized page is capped by the service, not by the caller's politeness.
	capped, err := f.service.ListTransactions(context.Background(), &dto.AdminTransactionFilter{PageSize: 5000})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if capped.PageSize != 100 {
		t.Fatalf("page_size = %d, want the cap 100", capped.PageSize)
	}
}
