package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// radreplySchema is the slice of the FreeRADIUS schema the write adapter needs.
// The real schema belongs to FreeRADIUS (ADR 0001), so the test creates only the
// table it touches and cleans up after itself.
const radreplySchema = `
CREATE TABLE IF NOT EXISTS radreply (
	id INT AUTO_INCREMENT PRIMARY KEY,
	username VARCHAR(64) NOT NULL DEFAULT '',
	attribute VARCHAR(64) NOT NULL DEFAULT '',
	op VARCHAR(2) NOT NULL DEFAULT '=',
	value VARCHAR(253) NOT NULL DEFAULT ''
)`

// TestRadiusWriter_WritesARealAccount is the narrow vendor seam for
// provisioning: it writes real rows to MySQL and reads them back through SQL, so
// the attributes, their operators and the epoch seconds are proven against a
// server rather than a fake (PRD Testing Decisions).
func TestRadiusWriter_WritesARealAccount(t *testing.T) {
	db := freeRADIUSTestDatabase(t)
	const phoneNumber = "628123450001"
	expiration := time.Date(2026, time.October, 24, 8, 0, 0, 0, time.UTC)

	writer := database.NewRadiusWriter(&database.FreeRADIUS{DB: db}, zap.NewNop())
	account := provisioning.Account{
		PhoneNumber: phoneNumber,
		PIN:         "123456",
		Expiration:  expiration,
		RateLimit:   "10M/10M",
	}
	if err := writer.Apply(context.Background(), account); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	check := radcheckValues(t, db, phoneNumber)
	if check["Cleartext-Password"] != ":=123456" {
		t.Fatalf("Cleartext-Password = %q, want :=123456", check["Cleartext-Password"])
	}
	wantExpiration := ":=1792828800"
	if check["Expiration"] != wantExpiration {
		t.Fatalf("Expiration = %q, want %q (epoch seconds)", check["Expiration"], wantExpiration)
	}
	if reply := radreplyValues(t, db, phoneNumber); reply["Mikrotik-Rate-Limit"] != "=10M/10M" {
		t.Fatalf("Mikrotik-Rate-Limit = %q, want =10M/10M", reply["Mikrotik-Rate-Limit"])
	}
}

// TestRadiusWriter_ExtendAndReapplyStaySingleRows covers the two shipped cases
// the writer owns: an Extend writes a later expiration and a new PIN over the
// same rows, and running the same write again changes nothing at all.
func TestRadiusWriter_ExtendAndReapplyStaySingleRows(t *testing.T) {
	db := freeRADIUSTestDatabase(t)
	const phoneNumber = "628123450002"

	writer := database.NewRadiusWriter(&database.FreeRADIUS{DB: db}, zap.NewNop())
	first := provisioning.Account{
		PhoneNumber: phoneNumber,
		PIN:         "111111",
		Expiration:  time.Date(2026, time.October, 24, 8, 0, 0, 0, time.UTC),
		RateLimit:   "5M/5M",
	}
	if err := writer.Apply(context.Background(), first); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	// An Extend: the Active Period was still running, so the service adds the new
	// Package duration on top and rotates the PIN.
	extended := first
	extended.PIN = "222222"
	extended.Expiration = first.Expiration.Add(24 * time.Hour)
	extended.RateLimit = "10M/10M"
	if err := writer.Apply(context.Background(), extended); err != nil {
		t.Fatalf("extend Apply: %v", err)
	}
	if err := writer.Apply(context.Background(), extended); err != nil {
		t.Fatalf("reapply Apply: %v", err)
	}

	assertSingleRow(t, db, "radcheck", phoneNumber, "Cleartext-Password")
	assertSingleRow(t, db, "radcheck", phoneNumber, "Expiration")
	assertSingleRow(t, db, "radreply", phoneNumber, "Mikrotik-Rate-Limit")

	check := radcheckValues(t, db, phoneNumber)
	if check["Cleartext-Password"] != ":=222222" {
		t.Fatalf("Cleartext-Password = %q, want the rotated PIN", check["Cleartext-Password"])
	}
	if check["Expiration"] != ":=1792915200" {
		t.Fatalf("Expiration = %q, want the extended day", check["Expiration"])
	}
	if reply := radreplyValues(t, db, phoneNumber); reply["Mikrotik-Rate-Limit"] != "=10M/10M" {
		t.Fatalf("Mikrotik-Rate-Limit = %q, want the newer Package limit", reply["Mikrotik-Rate-Limit"])
	}
}

// TestRadiusWriter_RestartsAnExpiredAccount pins the third case: an Active Period
// that already ended is replaced from the new settlement, not added to the old
// past expiration.
func TestRadiusWriter_RestartsAnExpiredAccount(t *testing.T) {
	db := freeRADIUSTestDatabase(t)
	const phoneNumber = "628123450003"

	writer := database.NewRadiusWriter(&database.FreeRADIUS{DB: db}, zap.NewNop())
	expired := provisioning.Account{
		PhoneNumber: phoneNumber,
		PIN:         "333333",
		Expiration:  time.Date(2026, time.October, 20, 8, 0, 0, 0, time.UTC),
		RateLimit:   "5M/5M",
	}
	if err := writer.Apply(context.Background(), expired); err != nil {
		t.Fatalf("Apply expired account: %v", err)
	}

	restarted := expired
	restarted.PIN = "444444"
	restarted.Expiration = time.Date(2026, time.October, 23, 9, 0, 0, 0, time.UTC)
	if err := writer.Apply(context.Background(), restarted); err != nil {
		t.Fatalf("Apply restarted account: %v", err)
	}

	check := radcheckValues(t, db, phoneNumber)
	if check["Expiration"] != ":=1792746000" {
		t.Fatalf("Expiration = %q, want the settlement-based expiration", check["Expiration"])
	}
	if check["Cleartext-Password"] != ":=444444" {
		t.Fatalf("Cleartext-Password = %q, want the new PIN", check["Cleartext-Password"])
	}
	assertSingleRow(t, db, "radcheck", phoneNumber, "Cleartext-Password")
	assertSingleRow(t, db, "radcheck", phoneNumber, "Expiration")
}

// freeRADIUSTestDatabase opens the CI MySQL instance and prepares the two tables
// this backend writes, skipping when no server is configured.
func freeRADIUSTestDatabase(t *testing.T) *sql.DB {
	t.Helper()

	deps := testutil.RequireDependencies(t)
	if deps.FreeRADIUSMySQLDSN == "" {
		t.Skip("TEST_FREERADIUS_MYSQL_DSN is not set; the MySQL adapter runs in CI")
	}

	db, err := sql.Open("mysql", deps.FreeRADIUSMySQLDSN)
	if err != nil {
		t.Fatalf("open freeradius mysql: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	for _, schema := range []string{radcheckSchema, radreplySchema} {
		if _, err := db.ExecContext(ctx, schema); err != nil {
			t.Fatalf("create freeradius table: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"radcheck", "radreply"} {
			_, _ = db.ExecContext(context.Background(),
				`DELETE FROM `+table+` WHERE username LIKE '62812345%'`)
		}
	})
	return db
}

// radcheckValues reads one attribute map back for a username, "op+value" so the
// test can pin the operator as well as the value.
func radcheckValues(t *testing.T, db *sql.DB, phoneNumber string) map[string]string {
	t.Helper()
	return attributeValues(t, db, "radcheck", phoneNumber)
}

func radreplyValues(t *testing.T, db *sql.DB, phoneNumber string) map[string]string {
	t.Helper()
	return attributeValues(t, db, "radreply", phoneNumber)
}

func attributeValues(t *testing.T, db *sql.DB, table, phoneNumber string) map[string]string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(),
		`SELECT attribute, op, value FROM `+table+` WHERE username = ?`, phoneNumber)
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()

	values := make(map[string]string)
	for rows.Next() {
		var attribute, op, value string
		if err := rows.Scan(&attribute, &op, &value); err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		values[attribute] = op + value
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s: %v", table, err)
	}
	return values
}

func assertSingleRow(t *testing.T, db *sql.DB, table, phoneNumber, attribute string) {
	t.Helper()

	var count int
	err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM `+table+` WHERE username = ? AND attribute = ?`,
		phoneNumber, attribute).Scan(&count)
	if err != nil {
		t.Fatalf("count %s rows: %v", table, err)
	}
	if count != 1 {
		t.Fatalf("%s has %d %q rows for %s, want exactly 1", table, count, attribute, phoneNumber)
	}
}
