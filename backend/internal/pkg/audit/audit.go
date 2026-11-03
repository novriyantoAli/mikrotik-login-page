// Package audit is the vocabulary two domains share to leave one trail row for a
// privileged action (ADR 0027). It owns the action names, the target types and
// the shape of the fact — and nothing else.
//
// The table itself belongs to the admin domain, so this package deliberately has
// no persistence: a domain that records an action declares its own small
// AuditRecorder port and depends on these values, never on admin (ADR 0027 §1).
// The action strings are stable: a report written today must still match a row
// written next year.
package audit

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
