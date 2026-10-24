package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
)

// The FreeRADIUS attributes this backend owns for a hotspot account (ADR 0004).
// CHAP needs the password recoverable, so it is stored as Cleartext-Password in
// FreeRADIUS; Postgres keeps only the encrypted copy (ADR 0016).
const (
	// The attribute *name* FreeRADIUS stores the password under, not a credential.
	attributePassword   = "Cleartext-Password" //nolint:gosec // FreeRADIUS attribute name
	attributeExpiration = "Expiration"
	attributeRateLimit  = "Mikrotik-Rate-Limit"

	opAssignment = ":="
	opReply      = "="
)

// radiusUpsert is the three statements that make one table write idempotent
// without depending on a unique index FreeRADIUS may not declare. The newest row
// for a (username, attribute) pair is updated; a missing pair is inserted.
type radiusUpsert struct {
	selectID string
	update   string
	insert   string
}

var (
	radcheckUpsert = radiusUpsert{
		selectID: `SELECT id FROM radcheck WHERE username = ? AND attribute = ? ORDER BY id DESC LIMIT 1`,
		update:   `UPDATE radcheck SET op = ?, value = ? WHERE id = ?`,
		insert:   `INSERT INTO radcheck (username, attribute, op, value) VALUES (?, ?, ?, ?)`,
	}
	radreplyUpsert = radiusUpsert{
		selectID: `SELECT id FROM radreply WHERE username = ? AND attribute = ? ORDER BY id DESC LIMIT 1`,
		update:   `UPDATE radreply SET op = ?, value = ? WHERE id = ?`,
		insert:   `INSERT INTO radreply (username, attribute, op, value) VALUES (?, ?, ?, ?)`,
	}
)

// RadiusWriter is the MySQL adapter for provisioning.Writer: it is the only code
// that knows which FreeRADIUS tables and attributes make up an account
// (ADR 0001). FreeRADIUS remains the owner of that schema.
type RadiusWriter struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewRadiusWriter builds the adapter over the FreeRADIUS connection.
func NewRadiusWriter(freeRADIUS *FreeRADIUS, logger *zap.Logger) *RadiusWriter {
	return &RadiusWriter{db: freeRADIUS.DB, logger: logger}
}

// Apply writes the account in one MySQL transaction, so a half-written account
// (password without expiration) is never visible to a login. Applying the same
// Account twice changes nothing (ADR 0016).
func (w *RadiusWriter) Apply(ctx context.Context, account provisioning.Account) error {
	if err := validateAccount(account); err != nil {
		return err
	}

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		w.logger.Error("begin freeradius account write", zap.Error(err))
		return fmt.Errorf("begin freeradius account write: %w", err)
	}
	defer w.rollback(tx)

	writes := []accountWrite{
		{radcheckUpsert, attributePassword, opAssignment, account.PIN},
		{radcheckUpsert, attributeExpiration, opAssignment,
			strconv.FormatInt(account.Expiration.Unix(), 10)},
		{radreplyUpsert, attributeRateLimit, opReply, account.RateLimit},
	}
	for _, write := range writes {
		if err := upsertAttribute(ctx, tx, write, account.PhoneNumber); err != nil {
			w.logger.Error("write freeradius attribute",
				zap.String("attribute", write.attribute),
				zap.Error(err),
			)
			return fmt.Errorf("write freeradius attribute %s: %w", write.attribute, err)
		}
	}

	if err := tx.Commit(); err != nil {
		w.logger.Error("commit freeradius account write", zap.Error(err))
		return fmt.Errorf("commit freeradius account write: %w", err)
	}
	return nil
}

// accountWrite is one attribute of the account and where it belongs.
type accountWrite struct {
	table     radiusUpsert
	attribute string
	op        string
	value     string
}

// rollback closes the transaction on every error path; a transaction already
// committed or rolled back reports ErrTxDone, which is not a fault.
func (w *RadiusWriter) rollback(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		w.logger.Warn("rollback freeradius account write", zap.Error(err))
	}
}

func upsertAttribute(
	ctx context.Context,
	tx *sql.Tx,
	write accountWrite,
	username string,
) error {
	var id int64
	err := tx.QueryRowContext(ctx, write.table.selectID, username, write.attribute).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, insertErr := tx.ExecContext(ctx, write.table.insert,
			username, write.attribute, write.op, write.value)
		if insertErr != nil {
			return fmt.Errorf("insert: %w", insertErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("select: %w", err)
	}

	if _, err := tx.ExecContext(ctx, write.table.update, write.op, write.value, id); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return nil
}

// validateAccount refuses an account FreeRADIUS could not authenticate with, so a
// bug in the service surfaces as an error instead of a silently empty login.
func validateAccount(account provisioning.Account) error {
	switch {
	case account.PhoneNumber == "":
		return errors.New("freeradius account needs a Phone Number")
	case account.PIN == "":
		return errors.New("freeradius account needs a PIN")
	case account.Expiration.IsZero():
		return errors.New("freeradius account needs an Expiration")
	case account.Expiration.Before(time.Unix(0, 0)):
		return errors.New("freeradius account Expiration is before the epoch")
	}
	return nil
}
