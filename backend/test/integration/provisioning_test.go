package integration

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/dto"
	transactionentity "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/entity"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// paidCheckout walks the real path from a session to a settled Transaction and
// returns the row the app polls, so each provisioning test starts from what the
// gateway actually produced.
func paidCheckout(t *testing.T, inputPhone, clientIP string) (*testutil.APIHarness, *http.Cookie, dto.TransactionResponse) {
	t.Helper()

	harness := testutil.NewAPIHarness(t)
	cookie := newSessionCookie(t, harness, inputPhone, clientIP)
	created := checkout(t, harness, cookie, firstPackageID(t, harness), "BRIVA")

	if code := postCallback(t, harness, created.Reference, payment.StatusPaid, created.AmountIDR,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("settle status = %d", code)
	}
	return harness, cookie, created
}

// TestProvisioning_PaidPurchaseBecomesAHotspotAccount is the slice's acceptance
// path: settlement alone does not touch the modem or MySQL, the worker does, and
// afterwards the buyer's Phone Number + PIN are a real FreeRADIUS account they can
// read again from the API.
func TestProvisioning_PaidPurchaseBecomesAHotspotAccount(t *testing.T) {
	harness, cookie, created := paidCheckout(t, "081200000041", "198.51.100.41")
	ctx := context.Background()

	// Settlement must not have provisioned anything yet, and must not have touched
	// the modem: that is the worker's job (PRD point 22).
	if accounts := harness.Doubles.Radius.Accounts(); len(accounts) != 0 {
		t.Fatalf("settlement applied %d accounts, want 0 before the worker runs", len(accounts))
	}
	if sent := harness.Doubles.SMS.Messages(); len(sent) != 0 {
		t.Fatalf("settlement sent %d SMS, want provisioning to queue it instead", len(sent))
	}

	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	response := getTransaction(t, harness, cookie, created.ID)
	if response.State != string(transactionentity.StateProvisioned) {
		t.Fatalf("state = %q, want provisioned", response.State)
	}
	if response.PhoneNumber != "6281200000041" {
		t.Fatalf("phone_number = %q, want the canonical session Phone Number", response.PhoneNumber)
	}
	if response.PIN == nil || len(*response.PIN) != 6 {
		t.Fatalf("pin = %v, want six digits on a provisioned Transaction", response.PIN)
	}

	accounts := harness.Doubles.Radius.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("writer applied %d accounts, want 1", len(accounts))
	}
	account := accounts[0]
	if account.PhoneNumber != response.PhoneNumber || account.PIN != *response.PIN {
		t.Fatalf("FreeRADIUS account %+v does not match the API's %q", account, response.PhoneNumber)
	}

	pkg, err := harness.Catalog.GetPackage(ctx, response.PackageID)
	if err != nil {
		t.Fatalf("read Package: %v", err)
	}
	if account.RateLimit != pkg.RateLimit {
		t.Fatalf("rate limit = %q, want the Package's %q", account.RateLimit, pkg.RateLimit)
	}
	if response.PaidAt == nil {
		t.Fatal("a provisioned Transaction must still carry paid_at")
	}
	paidAt, err := time.Parse(time.RFC3339, *response.PaidAt)
	if err != nil {
		t.Fatalf("parse paid_at: %v", err)
	}
	wantExpiration := paidAt.Add(time.Duration(pkg.DurationMinutes) * time.Minute)
	if !account.Expiration.Equal(wantExpiration) {
		t.Fatalf("expiration = %s, want settlement + duration (%s)", account.Expiration, wantExpiration)
	}

	// The modem is off as far as this test is concerned: the message waits in the
	// outbox and the PIN is still readable from the API (PRD point 30/32).
	rows := smsRows(t, harness, response.PhoneNumber)
	if len(rows) != 1 {
		t.Fatalf("outbox has %d rows, want 1 queued PIN", len(rows))
	}
	if rows[0].State != entity.StatePending {
		t.Fatalf("outbox state = %q, want pending until the worker delivers", rows[0].State)
	}
	if !bytes.Contains([]byte(rows[0].Message), []byte(*response.PIN)) {
		t.Fatalf("queued SMS %q does not carry the PIN", rows[0].Message)
	}

	assertPINIsNotStoredInClearText(t, harness, created.ID, *response.PIN)
}

// TestProvisioning_SecondPurchaseExtendsAndRotates covers Extend and PIN rotation
// end to end: the Active Period still running is added to, and the credential is
// new (PRD points 29 and 47).
func TestProvisioning_SecondPurchaseExtendsAndRotates(t *testing.T) {
	harness, cookie, first := paidCheckout(t, "081200000042", "198.51.100.42")
	ctx := context.Background()

	if err := harness.TransactionProvisioning.Provision(ctx, first.ID); err != nil {
		t.Fatalf("provision first purchase: %v", err)
	}
	firstAccount := harness.Doubles.Radius.Accounts()[0]
	// FreeRADIUS now holds that expiration; the fake reader stands in for it.
	harness.Doubles.RadiusReader.SetExpiration(firstAccount.PhoneNumber, firstAccount.Expiration)

	second := checkout(t, harness, cookie, firstPackageID(t, harness), "BRIVA")
	if code := postCallback(t, harness, second.Reference, payment.StatusPaid, second.AmountIDR,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("settle second purchase status = %d", code)
	}
	if err := harness.TransactionProvisioning.Provision(ctx, second.ID); err != nil {
		t.Fatalf("provision second purchase: %v", err)
	}

	accounts := harness.Doubles.Radius.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("writer applied %d accounts, want 2", len(accounts))
	}
	extended := accounts[1]
	pkg, err := harness.Catalog.GetPackage(ctx, second.PackageID)
	if err != nil {
		t.Fatalf("read Package: %v", err)
	}
	wantExpiration := firstAccount.Expiration.Add(time.Duration(pkg.DurationMinutes) * time.Minute)
	if !extended.Expiration.Equal(wantExpiration) {
		t.Fatalf("extended expiration = %s, want %s", extended.Expiration, wantExpiration)
	}
	if extended.PIN == firstAccount.PIN {
		t.Fatal("the second purchase reused the previous PIN")
	}

	pinValue, found, err := harness.TransactionService.CurrentPIN(ctx, first.PhoneNumber)
	if err != nil {
		t.Fatalf("current PIN: %v", err)
	}
	if !found || pinValue != extended.PIN {
		t.Fatalf("current PIN = (%q, %v), want the second purchase's %q",
			pinValue, found, extended.PIN)
	}

	if rows := smsRows(t, harness, extended.PhoneNumber); len(rows) != 2 {
		t.Fatalf("outbox has %d rows, want one per purchase", len(rows))
	}
}

// TestProvisioning_RetryReusesTheStoredPlan proves the cross-database
// compensation: the state a crash between the MySQL write and the Postgres commit
// leaves behind is a paid Transaction with a stored plan, and re-running the work
// applies exactly the same expiration and PIN instead of a second duration.
func TestProvisioning_RetryReusesTheStoredPlan(t *testing.T) {
	harness, cookie, created := paidCheckout(t, "081200000043", "198.51.100.43")
	ctx := context.Background()

	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	firstAccount := harness.Doubles.Radius.Accounts()[0]
	firstResponse := getTransaction(t, harness, cookie, created.ID)

	// Rewind to the crash state: the account is written, the Postgres commit is
	// not, so the Transaction is still paid and no SMS row exists yet.
	if err := harness.Postgres.Exec(
		`UPDATE transactions SET state = 'paid' WHERE id = ?`, created.ID).Error; err != nil {
		t.Fatalf("rewind transaction: %v", err)
	}
	if err := harness.Postgres.Exec(
		`DELETE FROM sms_outbox WHERE phone_number = ? AND message LIKE 'PIN Anda%'`,
		firstResponse.PhoneNumber,
	).Error; err != nil {
		t.Fatalf("clear the outbox: %v", err)
	}

	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("retry Provision: %v", err)
	}

	accounts := harness.Doubles.Radius.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("writer applied %d accounts, want 2 (one per attempt)", len(accounts))
	}
	if accounts[1] != firstAccount {
		t.Fatalf("the retry wrote a different account: %+v vs %+v", accounts[1], firstAccount)
	}

	retried := getTransaction(t, harness, cookie, created.ID)
	if retried.State != string(transactionentity.StateProvisioned) {
		t.Fatalf("state after retry = %q, want provisioned", retried.State)
	}
	if retried.PIN == nil || *retried.PIN != *firstResponse.PIN {
		t.Fatalf("PIN rotated on retry: %v vs %v", retried.PIN, firstResponse.PIN)
	}
	if rows := smsRows(t, harness, firstResponse.PhoneNumber); len(rows) != 1 {
		t.Fatalf("outbox has %d rows after the retry, want exactly 1", len(rows))
	}
}

// TestProvisioning_IsANoOpOnAnAlreadyProvisionedTransaction pins the cheapest
// guard: the sweep may look at the same row twice, and the second look does no
// work at all.
func TestProvisioning_IsANoOpOnAnAlreadyProvisionedTransaction(t *testing.T) {
	harness, cookie, created := paidCheckout(t, "081200000044", "198.51.100.44")
	ctx := context.Background()

	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("second Provision: %v", err)
	}

	if accounts := harness.Doubles.Radius.Accounts(); len(accounts) != 1 {
		t.Fatalf("writer applied %d accounts, want 1", len(accounts))
	}
	response := getTransaction(t, harness, cookie, created.ID)
	if rows := smsRows(t, harness, response.PhoneNumber); len(rows) != 1 {
		t.Fatalf("outbox has %d rows, want 1", len(rows))
	}
}

// smsRows reads the PIN messages queued for one Phone Number. The Magic Link of
// the login path is written to the same outbox, so the filter keeps this test
// about provisioning.
func smsRows(t *testing.T, harness *testutil.APIHarness, phoneNumber string) []entity.SMS {
	t.Helper()

	var rows []entity.SMS
	err := harness.Postgres.
		Where("phone_number = ? AND message LIKE ?", phoneNumber, "PIN Anda%").
		Order("id").
		Find(&rows).Error
	if err != nil {
		t.Fatalf("read sms_outbox: %v", err)
	}
	return rows
}

// assertPINIsNotStoredInClearText is the acceptance criterion about the database:
// the column holds ciphertext with a key version, never the digits (ADR 0016).
func assertPINIsNotStoredInClearText(
	t *testing.T,
	harness *testutil.APIHarness,
	transactionID uint,
	pinValue string,
) {
	t.Helper()

	var row struct {
		PINCiphertext []byte
		PINKeyVersion int
	}
	err := harness.Postgres.Raw(
		`SELECT pin_ciphertext, pin_key_version FROM transactions WHERE id = ?`, transactionID,
	).Scan(&row).Error
	if err != nil {
		t.Fatalf("read stored PIN: %v", err)
	}
	if len(row.PINCiphertext) == 0 {
		t.Fatal("the Transaction stores no PIN ciphertext")
	}
	if row.PINKeyVersion != harness.Config.PIN.ActiveKeyVersion {
		t.Fatalf("pin_key_version = %d, want the active version %d",
			row.PINKeyVersion, harness.Config.PIN.ActiveKeyVersion)
	}
	if bytes.Contains(row.PINCiphertext, []byte(pinValue)) {
		t.Fatal("the PIN is stored in clear text inside the ciphertext column")
	}
}

// TestProvisioning_RealFreeRADIUSExtendsTheActivePeriod is the one test that
// drives the whole Extend path against real MySQL: the production reader supplies
// the current Active Period and the production writer records the result, so the
// arithmetic is proven on the tables FreeRADIUS actually reads (PRD Testing
// Decisions).
func TestProvisioning_RealFreeRADIUSExtendsTheActivePeriod(t *testing.T) {
	db := freeRADIUSTestDatabase(t)
	harness := testutil.NewAPIHarness(t, testutil.WithRealRadiusReader(), testutil.WithRealRadiusWriter())
	ctx := context.Background()

	cookie := newSessionCookie(t, harness, "081200000045", "198.51.100.45")
	created := checkout(t, harness, cookie, firstPackageID(t, harness), "BRIVA")

	const phoneNumber = "6281200000045"
	activeUntil := harness.Doubles.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	seedRadiusAccount(t, db, phoneNumber, "111111", activeUntil, "1M/1M")

	if code := postCallback(t, harness, created.Reference, payment.StatusPaid, created.AmountIDR,
		testutil.ValidCallbackSignature); code != http.StatusOK {
		t.Fatalf("settle status = %d", code)
	}
	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	pkg, err := harness.Catalog.GetPackage(ctx, created.PackageID)
	if err != nil {
		t.Fatalf("read Package: %v", err)
	}
	wantExpiration := activeUntil.Add(time.Duration(pkg.DurationMinutes) * time.Minute)

	check := radcheckValues(t, db, phoneNumber)
	if want := ":=" + strconv.FormatInt(wantExpiration.Unix(), 10); check["Expiration"] != want {
		t.Fatalf("FreeRADIUS Expiration = %q, want %q", check["Expiration"], want)
	}
	if check["Cleartext-Password"] == ":=111111" {
		t.Fatal("the previous PIN survived the Extend")
	}
	if reply := radreplyValues(t, db, phoneNumber); reply["Mikrotik-Rate-Limit"] != "="+pkg.RateLimit {
		t.Fatalf("Mikrotik-Rate-Limit = %q, want the Package's %q",
			reply["Mikrotik-Rate-Limit"], "="+pkg.RateLimit)
	}
	for _, attribute := range []string{"Cleartext-Password", "Expiration"} {
		assertSingleRow(t, db, "radcheck", phoneNumber, attribute)
	}
	assertSingleRow(t, db, "radreply", phoneNumber, "Mikrotik-Rate-Limit")

	response := getTransaction(t, harness, cookie, created.ID)
	if response.PIN == nil || check["Cleartext-Password"] != ":="+*response.PIN {
		t.Fatalf("FreeRADIUS password %q does not match the API PIN %v",
			check["Cleartext-Password"], response.PIN)
	}
}

// seedRadiusAccount writes the account FreeRADIUS would already hold before an
// Extend, so the test starts from a running Active Period.
func seedRadiusAccount(
	t *testing.T,
	db *sql.DB,
	phoneNumber, pinValue string,
	expiration time.Time,
	rateLimit string,
) {
	t.Helper()

	ctx := context.Background()
	deleteRows := []string{
		`DELETE FROM radcheck WHERE username = ?`,
		`DELETE FROM radreply WHERE username = ?`,
	}
	for _, statement := range deleteRows {
		if _, err := db.ExecContext(ctx, statement, phoneNumber); err != nil {
			t.Fatalf("clear freeradius rows: %v", err)
		}
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO radcheck (username, attribute, op, value) VALUES (?, 'Cleartext-Password', ':=', ?)`,
		phoneNumber, pinValue); err != nil {
		t.Fatalf("seed radcheck password: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO radcheck (username, attribute, op, value) VALUES (?, 'Expiration', ':=', ?)`,
		phoneNumber, strconv.FormatInt(expiration.Unix(), 10)); err != nil {
		t.Fatalf("seed radcheck expiration: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO radreply (username, attribute, op, value) VALUES (?, 'Mikrotik-Rate-Limit', '=', ?)`,
		phoneNumber, rateLimit); err != nil {
		t.Fatalf("seed radreply rate limit: %v", err)
	}
}

// TestProvisioning_AttemptsAreCountedOnTheRow proves the counter behind the
// attempt log line and the operator metric survives a failure and a retry, so a
// stuck Transaction shows how often the worker tried (ADR 0018).
func TestProvisioning_AttemptsAreCountedOnTheRow(t *testing.T) {
	harness, _, created := paidCheckout(t, "081200000046", "198.51.100.46")
	ctx := context.Background()

	attempts := func() transactionentity.Transaction {
		t.Helper()

		var row transactionentity.Transaction
		if err := harness.Postgres.First(&row, created.ID).Error; err != nil {
			t.Fatalf("read Transaction: %v", err)
		}
		return row
	}

	if stored := attempts(); stored.ProvisionAttempts != 0 {
		t.Fatalf("attempts before provisioning = %d, want 0", stored.ProvisionAttempts)
	}

	harness.Doubles.Radius.Err = errors.New("freeradius unreachable")
	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err == nil {
		t.Fatal("Provision reported success while the FreeRADIUS writer was down")
	}
	failed := attempts()
	if failed.ProvisionAttempts != 1 || failed.State != transactionentity.StatePaid {
		t.Fatalf("after a failed attempt: attempts = %d, state = %q; want 1, paid",
			failed.ProvisionAttempts, failed.State)
	}

	harness.Doubles.Radius.Err = nil
	if err := harness.TransactionProvisioning.Provision(ctx, created.ID); err != nil {
		t.Fatalf("second Provision: %v", err)
	}
	succeeded := attempts()
	if succeeded.ProvisionAttempts != 2 || succeeded.State != transactionentity.StateProvisioned {
		t.Fatalf("after the retry: attempts = %d, state = %q; want 2, provisioned",
			succeeded.ProvisionAttempts, succeeded.State)
	}
}
