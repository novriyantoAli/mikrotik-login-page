package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/account/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	transactiondto "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	transactionentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

type accountEnvelope struct {
	Data dto.AccountResponse `json:"data"`
}

type accountHistoryEnvelope struct {
	Data       []transactiondto.TransactionSummaryResponse `json:"data"`
	TotalCount int64                                       `json:"total_count"`
	Page       int                                         `json:"page"`
	PageSize   int                                         `json:"page_size"`
}

// getAccount reads the Account Status summary through the production router.
func getAccount(t *testing.T, harness *testutil.APIHarness, cookie *http.Cookie) dto.AccountResponse {
	t.Helper()

	recorder := harness.DoRequest(t, http.MethodGet, "/api/account", nil,
		withCookie(cookie, "198.51.100.201"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/account = %d (%s)", recorder.Code, recorder.Body.String())
	}

	var envelope accountEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode account %q: %v", recorder.Body.String(), err)
	}
	return envelope.Data
}

func getHistory(
	t *testing.T,
	harness *testutil.APIHarness,
	cookie *http.Cookie,
	query string,
) accountHistoryEnvelope {
	t.Helper()

	recorder := harness.DoRequest(t, http.MethodGet, "/api/transactions"+query, nil,
		withCookie(cookie, "198.51.100.202"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/transactions%s = %d (%s)", query, recorder.Code, recorder.Body.String())
	}

	var envelope accountHistoryEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode history %q: %v", recorder.Body.String(), err)
	}
	return envelope
}

func getExtendPreview(
	t *testing.T,
	harness *testutil.APIHarness,
	cookie *http.Cookie,
	packageID uint,
) dto.ExtendPreviewResponse {
	t.Helper()

	path := "/api/account/extend-preview?package_id=" + uintString(packageID)
	recorder := harness.DoRequest(t, http.MethodGet, path, nil,
		withCookie(cookie, "198.51.100.203"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d (%s)", path, recorder.Code, recorder.Body.String())
	}

	var envelope struct {
		Data dto.ExtendPreviewResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode extend preview %q: %v", recorder.Body.String(), err)
	}
	return envelope.Data
}

func TestAccountStatus_RequiresASession(t *testing.T) {
	harness := testutil.NewAPIHarness(t)

	for _, path := range []string{
		"/api/account",
		"/api/account/extend-preview?package_id=1",
		"/api/transactions",
	} {
		recorder := harness.Do(t, http.MethodGet, path, nil)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s without a session = %d, want 401", path, recorder.Code)
		}
	}
}

// TestAccountStatus_NewBuyerSeesTheEmptyState pins the difference between "never
// bought" and "bought and it ran out": found=false with state "none" is the state
// that sends the app to the catalog (ADR 0017).
func TestAccountStatus_NewBuyerSeesTheEmptyState(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000101", "198.51.100.101")

	account := getAccount(t, harness, cookie)
	if account.PhoneNumber != "6281300000101" {
		t.Fatalf("phone_number = %q", account.PhoneNumber)
	}
	if account.ActivePeriod.Found {
		t.Fatalf("a new buyer has an Active Period: %+v", account.ActivePeriod)
	}
	if account.ActivePeriod.State != dto.StateNone {
		t.Fatalf("state = %q, want none", account.ActivePeriod.State)
	}
	if account.ActivePeriod.ExpiresAt != nil {
		t.Fatalf("expires_at = %q, want null", *account.ActivePeriod.ExpiresAt)
	}
	if account.Package != nil || account.PIN != nil {
		t.Fatalf("a new buyer has a Package or PIN: %+v", account)
	}

	history := getHistory(t, harness, cookie, "")
	if len(history.Data) != 0 || history.TotalCount != 0 {
		t.Fatalf("history = %+v, want an empty page", history)
	}
	if history.Page != 1 || history.PageSize != 20 {
		t.Fatalf("default page = %d/%d, want 1/20", history.Page, history.PageSize)
	}
}

// TestAccountStatus_ShowsTheCurrentAccountAndPIN is the slice's acceptance path:
// after provisioning, the session sees its Package, the exact expiration and the
// same PIN the account was written with. Nothing here names a Phone Number in the
// request: the session is the identity (PRD point 14).
func TestAccountStatus_ShowsTheCurrentAccountAndPIN(t *testing.T) {
	harness, cookie, created := paidCheckout(t, "081300000102", "198.51.100.102")
	ctx := context.Background()

	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	// The fake writer does not write FreeRADIUS, so the fake reader is told what
	// the real one would now hold.
	harness.Doubles.RadiusReader.SetExpiration("6281300000102",
		harness.Doubles.Radius.Accounts()[0].Expiration)

	account := getAccount(t, harness, cookie)
	if account.PhoneNumber != "6281300000102" {
		t.Fatalf("phone_number = %q, want the canonical session Phone Number", account.PhoneNumber)
	}
	if account.PIN == nil || *account.PIN != *getTransaction(t, harness, cookie, created.ID).PIN {
		t.Fatalf("pin = %v, want the provisioned Transaction's PIN", account.PIN)
	}

	pkg, err := harness.Catalog.GetPackage(ctx, created.PackageID)
	if err != nil {
		t.Fatalf("read Package: %v", err)
	}
	if account.Package == nil || account.Package.ID != pkg.ID ||
		account.Package.Name != pkg.Name || account.Package.RateLimit != pkg.RateLimit {
		t.Fatalf("package = %+v, want the catalog's %+v", account.Package, pkg)
	}

	applied := harness.Doubles.Radius.Accounts()[0]
	if !account.ActivePeriod.Found || account.ActivePeriod.ExpiresAt == nil {
		t.Fatalf("active_period = %+v, want the expiration FreeRADIUS holds", account.ActivePeriod)
	}
	if want := applied.Expiration.UTC().Format(time.RFC3339); *account.ActivePeriod.ExpiresAt != want {
		t.Fatalf("expires_at = %q, want %q", *account.ActivePeriod.ExpiresAt, want)
	}
	// The seeded Paket Harian is exactly 24 hours, and the frozen clock has not
	// moved since settlement: exactly the warning window is left, and `<=` says
	// that is "expiring" — the same boundary the Login Page uses (ADR 0017).
	if account.ActivePeriod.State != dto.StateExpiring {
		t.Fatalf("state = %q, want expiring at exactly the 24h boundary", account.ActivePeriod.State)
	}
}

// TestAccountStatus_NearExpiryUsesTheConfiguredThreshold is the clock proof the
// handoff asks for: 23 hours before the end is expiring, 25 hours is active, and
// a passed expiration is expired, all without sleeping.
func TestAccountStatus_NearExpiryUsesTheConfiguredThreshold(t *testing.T) {
	harness, cookie, created := paidCheckout(t, "081300000103", "198.51.100.103")
	ctx := context.Background()
	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	now := harness.Doubles.Clock.Now()
	phoneNumber := "6281300000103"

	cases := []struct {
		name      string
		expiresAt time.Time
		state     dto.ActivePeriodState
	}{
		{name: "23 hours left", expiresAt: now.Add(23 * time.Hour), state: dto.StateExpiring},
		{name: "25 hours left", expiresAt: now.Add(25 * time.Hour), state: dto.StateActive},
		{name: "one hour ago", expiresAt: now.Add(-time.Hour), state: dto.StateExpired},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			harness.Doubles.RadiusReader.SetExpiration(phoneNumber, testCase.expiresAt)

			account := getAccount(t, harness, cookie)
			if account.ActivePeriod.State != testCase.state {
				t.Fatalf("state = %q, want %q at %s before the end",
					account.ActivePeriod.State, testCase.state, testCase.expiresAt.Sub(now))
			}
			if !account.ActivePeriod.Found || account.ActivePeriod.ExpiresAt == nil {
				t.Fatalf("active_period = %+v, want a found expiration", account.ActivePeriod)
			}
			if got, want := *account.ActivePeriod.ExpiresAt, testCase.expiresAt.UTC().Format(time.RFC3339); got != want {
				t.Fatalf("expires_at = %q, want %q", got, want)
			}
		})
	}
}

// TestAccountStatus_HistoryIsPaginatedNewestFirstWithoutPIN covers the history
// shape: every Transaction is listed, pagination counts the whole set, and no row
// ever carries a credential (ADR 0017).
func TestAccountStatus_HistoryIsPaginatedNewestFirstWithoutPIN(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000104", "198.51.100.104")
	packageID := firstPackageID(t, harness)

	ids := make([]uint, 0, 3)
	for index := 0; index < 3; index++ {
		ids = append(ids, checkout(t, harness, cookie, packageID, "BRIVA").ID)
		// Past the payment deadline, so the next checkout opens a new Transaction
		// instead of collapsing into the still-pending one.
		harness.Doubles.Clock.Advance(2 * time.Hour)
	}

	firstPage := getHistory(t, harness, cookie, "?page=1&page_size=2")
	if firstPage.TotalCount != 3 {
		t.Fatalf("total_count = %d, want 3", firstPage.TotalCount)
	}
	if firstPage.Page != 1 || firstPage.PageSize != 2 {
		t.Fatalf("page/page_size = %d/%d, want 1/2", firstPage.Page, firstPage.PageSize)
	}
	if len(firstPage.Data) != 2 {
		t.Fatalf("page 1 returned %d rows, want 2", len(firstPage.Data))
	}
	if firstPage.Data[0].ID != ids[2] || firstPage.Data[1].ID != ids[1] {
		t.Fatalf("history is not newest first: %+v", firstPage.Data)
	}

	secondPage := getHistory(t, harness, cookie, "?page=2&page_size=2")
	if len(secondPage.Data) != 1 || secondPage.Data[0].ID != ids[0] {
		t.Fatalf("page 2 = %+v, want the oldest Transaction", secondPage.Data)
	}

	for _, row := range append(firstPage.Data, secondPage.Data...) {
		if row.AmountIDR != 5000 || row.PackageName == "" || row.CreatedAt == "" {
			t.Fatalf("history row is incomplete: %+v", row)
		}
		if row.State != string(transactionentity.StateExpired) &&
			row.State != string(transactionentity.StatePending) {
			t.Fatalf("history row state = %q", row.State)
		}
	}
}

// TestAccountStatus_AnotherPhoneNumberIsNotFound proves the isolation criterion at
// the HTTP boundary: naming a foreign Phone Number in the query, or asking for its
// Transaction, answers 404 and never 403.
func TestAccountStatus_AnotherPhoneNumberIsNotFound(t *testing.T) {
	harness, _, transaction := paidCheckout(t, "081300000105", "198.51.100.105")
	if err := harness.TransactionProvisioning.Provision(context.Background(), transaction.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	stranger := newSessionCookie(t, harness, "081300000106", "198.51.100.106")
	// The owner's PIN is readable to the owner and never through a name in the URL.
	if pin, found, err := harness.TransactionService.CurrentPIN(context.Background(), "6281300000105"); err != nil || !found || pin == "" {
		t.Fatalf("CurrentPIN = %q, %v, %v", pin, found, err)
	}

	for _, path := range []string{
		"/api/account?phone_number=6281300000105",
		"/api/transactions?phone_number=6281300000105",
		"/api/transactions?phone_number=0813-0000-0105",
		"/api/transactions/" + uintString(transaction.ID),
		"/api/account/extend-preview?package_id=1&phone_number=6281300000105",
	} {
		recorder := harness.DoRequest(t, http.MethodGet, path, nil,
			withCookie(stranger, "198.51.100.206"))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("GET %s as another Phone Number = %d (%s), want 404",
				path, recorder.Code, recorder.Body.String())
		}
	}
}

// TestExtendPreview_MatchesTheExpirationProvisioningApplies is the one-rule proof:
// the number the confirmation showed is the number provisioning wrote, because both
// call provisioning.Target (ADR 0017).
func TestExtendPreview_MatchesTheExpirationProvisioningApplies(t *testing.T) {
	harness, cookie, first := paidCheckout(t, "081300000107", "198.51.100.107")
	ctx := context.Background()

	if err := harness.TransactionProvisioning.Provision(ctx, first.ID); err != nil {
		t.Fatalf("provision first purchase: %v", err)
	}
	running := harness.Doubles.Radius.Accounts()[0].Expiration
	harness.Doubles.RadiusReader.SetExpiration("6281300000107", running)

	preview := getExtendPreview(t, harness, cookie, firstPackageID(t, harness))
	if !preview.Extends {
		t.Fatal("a running Active Period must be reported as an Extend")
	}
	if preview.CurrentExpiration == nil ||
		*preview.CurrentExpiration != running.UTC().Format(time.RFC3339) {
		t.Fatalf("current_expiration = %v, want %s", preview.CurrentExpiration, running)
	}

	second := checkout(t, harness, cookie, firstPackageID(t, harness), "BRIVA")
	if code := postCallback(t, harness, second.Reference, payment.StatusPaid, second.AmountIDR,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("settle second purchase = %d", code)
	}
	if err := harness.TransactionProvisioning.Provision(ctx, second.ID); err != nil {
		t.Fatalf("provision second purchase: %v", err)
	}

	applied := harness.Doubles.Radius.Accounts()[1].Expiration
	harness.Doubles.RadiusReader.SetExpiration("6281300000107", applied)
	if want := applied.UTC().Format(time.RFC3339); preview.EstimatedExpiration != want {
		t.Fatalf("preview said %s, provisioning applied %s",
			preview.EstimatedExpiration, want)
	}

	account := getAccount(t, harness, cookie)
	if account.ActivePeriod.ExpiresAt == nil ||
		*account.ActivePeriod.ExpiresAt != preview.EstimatedExpiration {
		t.Fatalf("Account Status shows %v, want the preview's %s",
			account.ActivePeriod.ExpiresAt, preview.EstimatedExpiration)
	}
}

// TestExtendPreview_WithNothingRunningStartsFromNow covers the other half of PRD
// point 21: a buyer with no Active Period is not extending anything.
func TestExtendPreview_WithNothingRunningStartsFromNow(t *testing.T) {
	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, "081300000108", "198.51.100.108")

	preview := getExtendPreview(t, harness, cookie, firstPackageID(t, harness))
	if preview.Extends {
		t.Fatal("a buyer with no Active Period is not extending anything")
	}
	if preview.CurrentExpiration != nil {
		t.Fatalf("current_expiration = %v, want null", *preview.CurrentExpiration)
	}

	want := harness.Doubles.Clock.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	if preview.EstimatedExpiration != want {
		t.Fatalf("estimated_expiration = %q, want now + the Package duration (%s)",
			preview.EstimatedExpiration, want)
	}
}
