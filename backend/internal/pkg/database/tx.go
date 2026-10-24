package database

import "gorm.io/gorm"

// Tx is one caller-owned Postgres transaction. It exists so a domain service can
// hand its transaction to another domain's repository without either service
// importing gorm: provisioning has to commit the Transaction's new state and the
// PIN SMS row together, and the outbox row belongs to the notification domain
// (ADR 0010/0016).
//
// A service treats it as opaque; only repositories unwrap it.
type Tx struct {
	db *gorm.DB
}

// NewTx wraps a GORM transaction handle. Repositories call it while opening a
// transaction; nothing else should.
func NewTx(db *gorm.DB) Tx { return Tx{db: db} }

// DB returns the handle a repository must use to run a statement in the
// caller's transaction.
func (t Tx) DB() *gorm.DB { return t.db }
