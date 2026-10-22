package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

// radcheckSchema is the slice of the FreeRADIUS schema the read adapter needs.
// The real schema is owned by FreeRADIUS (ADR 0001); the test creates only the
// table it writes to, so it can run against CI's empty MySQL service.
const radcheckSchema = `
CREATE TABLE IF NOT EXISTS radcheck (
	id INT AUTO_INCREMENT PRIMARY KEY,
	username VARCHAR(64) NOT NULL DEFAULT '',
	attribute VARCHAR(64) NOT NULL DEFAULT '',
	op VARCHAR(2) NOT NULL DEFAULT '==',
	value VARCHAR(253) NOT NULL DEFAULT ''
)`

// TestRadiusExpirationReaderReadsFreeRADIUS is the narrow vendor seam for the
// Active Period read: it writes real rows to MySQL and reads them back through
// the adapter, which is the only code that knows the table and attribute.
func TestRadiusExpirationReaderReadsFreeRADIUS(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, radcheckSchema); err != nil {
		t.Fatalf("create radcheck: %v", err)
	}

	const phoneNumber = "628123456789"
	if _, err := db.ExecContext(ctx,
		`DELETE FROM radcheck WHERE username = ?`, phoneNumber); err != nil {
		t.Fatalf("clear radcheck: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM radcheck WHERE username = ?`, phoneNumber)
	})

	reader := database.NewRadiusExpirationReader(&database.FreeRADIUS{DB: db}, zap.NewNop())

	expiration, err := reader.Expiration(ctx, phoneNumber)
	if err != nil {
		t.Fatalf("Expiration on an empty account: %v", err)
	}
	if expiration.Found {
		t.Fatal("an account without an Expiration row must report Found = false")
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO radcheck (username, attribute, op, value) VALUES (?, 'Expiration', ':=', ?)`,
		phoneNumber, "1768000000"); err != nil {
		t.Fatalf("seed radcheck: %v", err)
	}

	expiration, err = reader.Expiration(ctx, phoneNumber)
	if err != nil {
		t.Fatalf("Expiration: %v", err)
	}
	if !expiration.Found {
		t.Fatal("Found = false, want true")
	}
	if !expiration.ExpiresAt.Equal(time.Unix(1768000000, 0)) {
		t.Fatalf("expiry = %s, want %s", expiration.ExpiresAt, time.Unix(1768000000, 0).UTC())
	}
	if expiration.ExpiresAt.Location() != time.UTC {
		t.Fatalf("expiry location = %s, want UTC", expiration.ExpiresAt.Location())
	}

	// A malformed value is data corruption, not "no Active Period".
	if _, err := db.ExecContext(ctx,
		`UPDATE radcheck SET value = 'not-epoch-seconds' WHERE username = ?`, phoneNumber); err != nil {
		t.Fatalf("corrupt radcheck: %v", err)
	}
	if _, err := reader.Expiration(ctx, phoneNumber); err == nil {
		t.Fatal("expected an error for a malformed Expiration value")
	}

	// A free port is how the harness points the app at an unreachable database.
	closed, err := sql.Open("mysql", "radius:radius@tcp(127.0.0.1:1)/radius?parseTime=true")
	if err != nil {
		t.Fatalf("open closed mysql: %v", err)
	}
	t.Cleanup(func() { _ = closed.Close() })

	unreachable := database.NewRadiusExpirationReader(&database.FreeRADIUS{DB: closed}, zap.NewNop())
	if _, err := unreachable.Expiration(ctx, phoneNumber); err == nil {
		t.Fatal("expected an error when FreeRADIUS MySQL is unreachable")
	}
}
