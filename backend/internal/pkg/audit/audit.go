// Package audit is the vocabulary two domains share to leave one trail row for a
// privileged action (ADR 0027). It owns the action names, the target types and
// the shape of the fact — and nothing else.
//
// The table itself belongs to the admin domain, so this package has no
// persistence of its own: the Recorder interface declared here depends only on the
// opaque database.Tx, never on gorm or a table, and no domain imports admin
// (ADR 0027 §1). The action strings are stable: a report written today must still
// match a row written next year.
package audit

import (
	"context"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
)

// Recorder appends exactly one trail row for a privileged action. It lives here,
// with the vocabulary, so every domain depends on one interface instead of
// declaring its own copy; the table and the adapter stay in the admin domain,
// which no other domain imports (ADR 0027 §1).
//
// It takes the caller's transaction rather than opening its own, so the change
// and its witness commit or roll back together and "changed without a trail row"
// is not representable (ADR 0027 §2). A zero-value database.Tx — "no
// transaction" — is refused.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, entry Entry) error
}

// Action names one audited act.
type Action string

// Every action the panel can leave behind (ADR 0027 §3).
const (
	ActionPackageCreate      Action = "package.create"
	ActionPackageUpdate      Action = "package.update"
	ActionPackageActivate    Action = "package.activate"
	ActionPackageDeactivate  Action = "package.deactivate"
	ActionPINReveal          Action = "pin.reveal"
	ActionAdminCreate        Action = "admin.create"
	ActionAdminUpdate        Action = "admin.update"
	ActionAdminPasswordReset Action = "admin.password_reset"
	ActionAdminDeactivate    Action = "admin.deactivate"
)

// Actions lists every action the vocabulary knows, in one place. The reader
// validates a filter against this list instead of copying the strings, so an
// action added above is readable the day it starts being written (ADR 0027 §3).
func Actions() []Action {
	return []Action{
		ActionPackageCreate,
		ActionPackageUpdate,
		ActionPackageActivate,
		ActionPackageDeactivate,
		ActionPINReveal,
		ActionAdminCreate,
		ActionAdminUpdate,
		ActionAdminPasswordReset,
		ActionAdminDeactivate,
	}
}

// Known reports whether action is one the vocabulary names. An unknown value is a
// typo, and a reader must answer it rather than match nothing: filtering on a
// misspelled action would look like "the trail is empty".
func Known(action Action) bool {
	for _, known := range Actions() {
		if known == action {
			return true
		}
	}
	return false
}

// TargetType names what an audited row points at. It has its own type for the same
// reason Action does: a typo in a target is a reader that silently matches nothing.
type TargetType string

// The target vocabulary. They are as stable as Action: the reader filters on them.
const (
	TargetPackage     TargetType = "package"
	TargetTransaction TargetType = "transaction"
	TargetAdmin       TargetType = "admin"
)

// Actor is who performed the act, as the server knew them at the time. Email is
// copied into the row rather than joined, so the trail still reads after the
// admin it names is deactivated or renamed (ADR 0027 §3). IP is where the action
// came from.
type Actor struct {
	ID    uint
	Email string
	IP    string
}

// Entry is one fact to append. It carries no secrets — a PIN is never written
// here (ADR 0027 §4) — and no timestamps: the row's created_at is the database's
// answer to when the transaction committed.
type Entry struct {
	Actor      Actor
	Action     Action
	TargetType TargetType
	// TargetID is the identity of the row the action changed, as text so one
	// column can hold a Package id and a Phone Number alike.
	TargetID string
	// Payload is the small document a reader needs to answer "what changed":
	// before/after for a catalog edit, the Transaction reference for a reveal.
	Payload map[string]any
}

// WithTarget returns a copy of the entry pointing at another target. It exists so
// a repository that only learns the identity after the insert (the new admin's
// id, the new Package's id) can complete the fact without rebuilding it.
func (e Entry) WithTarget(targetType TargetType, targetID string) Entry {
	e.TargetType = targetType
	e.TargetID = targetID
	return e
}
