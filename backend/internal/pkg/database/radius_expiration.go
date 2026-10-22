package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
)

// radiusExpirationQuery reads the Expiration check attribute FreeRADIUS applies
// to a username. The newest row wins if an account somehow has more than one;
// the backend's own writer keeps a single row per username (ADR 0001).
const radiusExpirationQuery = `
SELECT value
FROM radcheck
WHERE username = ?
  AND attribute = 'Expiration'
ORDER BY id DESC
LIMIT 1`

// RadiusExpirationReader is the MySQL adapter for
// provisioning.ExpirationReader. It is the only code that knows the FreeRADIUS
// table and attribute that define the end of an Active Period (ADR 0014).
type RadiusExpirationReader struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewRadiusExpirationReader builds the adapter over the FreeRADIUS connection.
func NewRadiusExpirationReader(freeRADIUS *FreeRADIUS, logger *zap.Logger) *RadiusExpirationReader {
	return &RadiusExpirationReader{db: freeRADIUS.DB, logger: logger}
}

// Expiration implements provisioning.ExpirationReader. FreeRADIUS stores the
// expiration as epoch seconds; an unknown username is not an error.
func (r *RadiusExpirationReader) Expiration(
	ctx context.Context,
	phoneNumber string,
) (provisioning.Expiration, error) {
	var value string
	err := r.db.QueryRowContext(ctx, radiusExpirationQuery, phoneNumber).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return provisioning.Expiration{}, nil
	}
	if err != nil {
		r.logger.Error("read freeradius expiration", zap.Error(err))
		return provisioning.Expiration{}, fmt.Errorf("read freeradius expiration: %w", err)
	}

	epoch, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		// A malformed value is a data fault, not a missing account: report it
		// rather than pretending the user has no Active Period.
		r.logger.Error("freeradius expiration is not epoch seconds", zap.String("value", value), zap.Error(err))
		return provisioning.Expiration{}, fmt.Errorf("parse freeradius expiration %q: %w", value, err)
	}

	return provisioning.Expiration{
		ExpiresAt: time.Unix(epoch, 0).UTC(),
		Found:     true,
	}, nil
}
