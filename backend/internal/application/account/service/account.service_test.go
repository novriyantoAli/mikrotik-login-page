package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/service"
	packagesdto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/dto"
	packagesentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/entity"
	packageservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	transactiondto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
)

const (
	testPhone   = "6281200000001"
	testPIN     = "123456"
	dayDuration = 24 * time.Hour
)

var testNow = time.Date(2026, time.March, 1, 8, 0, 0, 0, time.UTC)

// fixedClock is the injected time: Account Status never calls time.Now.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type stubTransactions struct {
	account *transactionservice.CurrentAccount
	err     error
}

func (s *stubTransactions) Channels(context.Context) ([]transactiondto.ChannelResponse, error) {
	return nil, nil
}

func (s *stubTransactions) Create(
	context.Context,
	string,
	*transactiondto.CreateTransactionRequest,
) (*transactiondto.TransactionResponse, error) {
	return nil, nil
}

func (s *stubTransactions) Get(
	context.Context,
	string,
	uint,
) (*transactiondto.TransactionResponse, error) {
	return nil, nil
}

func (s *stubTransactions) HandleCallback(context.Context, []byte, string) error { return nil }

func (s *stubTransactions) CurrentPIN(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (s *stubTransactions) CurrentAccount(
	context.Context,
	string,
) (*transactionservice.CurrentAccount, error) {
	return s.account, s.err
}

func (s *stubTransactions) History(
	context.Context,
	string,
	*transactiondto.TransactionFilter,
) (*transactiondto.TransactionPage, error) {
	return nil, nil
}

type stubCatalog struct {
	pkg *packagesentity.Package
	err error
}

func (c *stubCatalog) ListPackages(context.Context) ([]packagesdto.PackageResponse, error) {
	return nil, nil
}

func (c *stubCatalog) GetPackage(context.Context, uint) (*packagesentity.Package, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.pkg, nil
}

func (c *stubCatalog) Seed(context.Context) (int64, error) { return 0, nil }

type stubExpirations struct {
	current provisioning.Expiration
	err     error
}

func (s *stubExpirations) Expiration(context.Context, string) (provisioning.Expiration, error) {
	return s.current, s.err
}

func newAccountService(
	transactions *stubTransactions,
	catalog *stubCatalog,
	expirations *stubExpirations,
) service.Service {
	return service.NewService(
		transactions,
		catalog,
		expirations,
		fixedClock{now: testNow},
		service.Settings{ExpiryWarning: 24 * time.Hour},
		zap.NewNop(),
	)
}

func accountPackage(durationMinutes int) *packagesentity.Package {
	return &packagesentity.Package{
		ID: 7, Name: "Paket Harian", PriceIDR: 5000,
		DurationMinutes: durationMinutes, RateLimit: "5M/5M",
	}
}

// TestSummary_ComputesTheWarningBoundary is the service-level proof of ADR 0017's
// threshold, including the exact boundary the Login Page uses (`<=`).
func TestSummary_ComputesTheWarningBoundary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		current   provisioning.Expiration
		wantState dto.ActivePeriodState
	}{
		{
			name:      "23 hours left is almost over",
			current:   provisioning.Expiration{ExpiresAt: testNow.Add(23 * time.Hour), Found: true},
			wantState: dto.StateExpiring,
		},
		{
			name:      "exactly the warning window is almost over",
			current:   provisioning.Expiration{ExpiresAt: testNow.Add(24 * time.Hour), Found: true},
			wantState: dto.StateExpiring,
		},
		{
			name:      "25 hours left is active",
			current:   provisioning.Expiration{ExpiresAt: testNow.Add(25 * time.Hour), Found: true},
			wantState: dto.StateActive,
		},
		{
			name:      "a passed expiration is expired",
			current:   provisioning.Expiration{ExpiresAt: testNow.Add(-time.Minute), Found: true},
			wantState: dto.StateExpired,
		},
		{
			name:      "no expiration at all is none",
			current:   provisioning.Expiration{},
			wantState: dto.StateNone,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			accounts := newAccountService(
				&stubTransactions{},
				&stubCatalog{},
				&stubExpirations{current: testCase.current},
			)

			summary, err := accounts.Summary(context.Background(), testPhone)
			if err != nil {
				t.Fatalf("Summary: %v", err)
			}
			if summary.ActivePeriod.State != testCase.wantState {
				t.Fatalf("state = %q, want %q", summary.ActivePeriod.State, testCase.wantState)
			}
			if testCase.current.Found && summary.ActivePeriod.ExpiresAt == nil {
				t.Fatal("a found expiration must be rendered")
			}
			if !testCase.current.Found && summary.ActivePeriod.ExpiresAt != nil {
				t.Fatalf("expires_at = %q, want null", *summary.ActivePeriod.ExpiresAt)
			}
		})
	}
}

func TestSummary_ReturnsTheCurrentPINAndPackage(t *testing.T) {
	t.Parallel()

	accounts := newAccountService(
		&stubTransactions{account: &transactionservice.CurrentAccount{
			TransactionID: 4, PackageID: 7, PIN: testPIN,
		}},
		&stubCatalog{pkg: accountPackage(24 * 60)},
		&stubExpirations{current: provisioning.Expiration{ExpiresAt: testNow.Add(dayDuration), Found: true}},
	)

	summary, err := accounts.Summary(context.Background(), testPhone)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.PIN == nil || *summary.PIN != testPIN {
		t.Fatalf("pin = %v, want %q", summary.PIN, testPIN)
	}
	if summary.Package == nil || summary.Package.ID != 7 || summary.Package.RateLimit != "5M/5M" {
		t.Fatalf("package = %+v", summary.Package)
	}
	if summary.PhoneNumber != testPhone {
		t.Fatalf("phone_number = %q", summary.PhoneNumber)
	}
}

// TestSummary_KeepsThePINWhenTheCatalogLostThePackage covers the tolerated case:
// a removed catalog row must not turn a buyer's page into the empty state.
func TestSummary_KeepsThePINWhenTheCatalogLostThePackage(t *testing.T) {
	t.Parallel()

	accounts := newAccountService(
		&stubTransactions{account: &transactionservice.CurrentAccount{
			TransactionID: 4, PackageID: 7, PIN: testPIN,
		}},
		&stubCatalog{err: packageservice.ErrPackageNotFound},
		&stubExpirations{current: provisioning.Expiration{ExpiresAt: testNow.Add(25 * time.Hour), Found: true}},
	)

	summary, err := accounts.Summary(context.Background(), testPhone)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.PIN == nil || *summary.PIN != testPIN {
		t.Fatalf("pin = %v, want the buyer's PIN", summary.PIN)
	}
	if summary.Package != nil {
		t.Fatalf("package = %+v, want null", summary.Package)
	}
	if summary.ActivePeriod.State != dto.StateActive {
		t.Fatalf("state = %q, want active", summary.ActivePeriod.State)
	}
}

func TestSummary_ReportsAnUnreadableActivePeriod(t *testing.T) {
	t.Parallel()

	accounts := newAccountService(
		&stubTransactions{},
		&stubCatalog{},
		&stubExpirations{err: errors.New("mysql down")},
	)

	if _, err := accounts.Summary(context.Background(), testPhone); err == nil {
		t.Fatal("an unreadable Active Period must fail instead of inventing a state")
	}
}

func TestExtendPreview_ExtendsTheRunningActivePeriod(t *testing.T) {
	t.Parallel()

	running := testNow.Add(12 * time.Hour)
	accounts := newAccountService(
		&stubTransactions{},
		&stubCatalog{pkg: accountPackage(24 * 60)},
		&stubExpirations{current: provisioning.Expiration{ExpiresAt: running, Found: true}},
	)

	preview, err := accounts.ExtendPreview(context.Background(), testPhone, 7)
	if err != nil {
		t.Fatalf("ExtendPreview: %v", err)
	}
	if !preview.Extends {
		t.Fatal("a running Active Period must be reported as an Extend")
	}
	if preview.EstimatedExpiration != running.Add(dayDuration).Format(time.RFC3339) {
		t.Fatalf("estimated_expiration = %q, want the running end plus the duration",
			preview.EstimatedExpiration)
	}
	if preview.CurrentExpiration == nil || *preview.CurrentExpiration != running.Format(time.RFC3339) {
		t.Fatalf("current_expiration = %v", preview.CurrentExpiration)
	}
}

func TestExtendPreview_StartsFromNowWhenNothingIsRunning(t *testing.T) {
	t.Parallel()

	accounts := newAccountService(
		&stubTransactions{},
		&stubCatalog{pkg: accountPackage(24 * 60)},
		&stubExpirations{},
	)

	preview, err := accounts.ExtendPreview(context.Background(), testPhone, 7)
	if err != nil {
		t.Fatalf("ExtendPreview: %v", err)
	}
	if preview.Extends || preview.CurrentExpiration != nil {
		t.Fatalf("preview = %+v, want a fresh start", preview)
	}
	if want := testNow.Add(dayDuration).Format(time.RFC3339); preview.EstimatedExpiration != want {
		t.Fatalf("estimated_expiration = %q, want %q", preview.EstimatedExpiration, want)
	}
}

func TestExtendPreview_UnknownPackageIsNotEstimated(t *testing.T) {
	t.Parallel()

	accounts := newAccountService(
		&stubTransactions{},
		&stubCatalog{err: packageservice.ErrPackageNotFound},
		&stubExpirations{},
	)

	if _, err := accounts.ExtendPreview(context.Background(), testPhone, 99); !errors.Is(err, service.ErrPackageUnknown) {
		t.Fatalf("err = %v, want ErrPackageUnknown", err)
	}
}
