package database

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// ErrNoTransaction means a caller asked to run work inside a transaction without
// handing in a database handle. It is a programming error, not a request failure.
var ErrNoTransaction = errors.New("within transaction: no database handle")

// Tx is one caller-owned Postgres transaction. It exists so a domain service can
// hand its transaction to another domain's repository without either service
// importing gorm: provisioning has to commit the Transaction's new state and the
// PIN SMS row together, and the outbox row belongs to the notification domain
// (ADR 0010/0016).
//
// A service treats it as opaque; only repositories unwrap it. The zero value
// means "no transaction", which is why it is the only value a caller can build:
// the field is unexported and WithinTx is the only constructor, so every non-zero
// Tx really is a live transaction (ADR 0027 §2).
type Tx struct {
	db *gorm.DB
}

// WithinTx runs fn inside one database transaction and hands it the only kind of
// Tx that can exist: a live one. Returning an error rolls the whole thing back;
// returning nil commits it. Repositories are the callers; a service never opens a
// transaction itself.
func WithinTx(ctx context.Context, db *gorm.DB, fn func(Tx) error) error {
	if db == nil {
		return ErrNoTransaction
	}
	return db.WithContext(ctx).Transaction(func(handle *gorm.DB) error {
		return fn(Tx{db: handle})
	})
}

// DB returns the handle a repository must use to run a statement in the caller's
// transaction. It is nil on the zero value, which is how a recorder can tell a
// real transaction from a mistaken one.
func (t Tx) DB() *gorm.DB { return t.db }
