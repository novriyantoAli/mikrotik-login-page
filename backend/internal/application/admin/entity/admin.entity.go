// Package entity holds the persisted shape of the admin domain: the panel's own
// identity, its sessions and its append-only audit trail. All three are separate
// from the buyer's Phone Number identity on purpose, so a buyer session can never
// become an admin session (ADR 0024, ADR 0027).
package entity

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Role is what an admin may do. The strings travel to the panel and into the
// permission matrix, so they are stable values, not display text (ADR 0024 §3).
type Role string

// The three roles of the permission matrix.
const (
	RoleOwner   Role = "owner"
	RoleSupport Role = "support"
	RoleAuditor Role = "auditor"
)

// Admin is one panel actor. Email is canonical (trimmed, lowercased) before it is
// ever written, which is what makes the unique index an identity rather than a
// spelling: `Owner@Example.test` and `owner@example.test` are one admin.
//
// PasswordHash is bcrypt cost 12. IsActive is checked on every session lookup:
// deactivating an admin is a server-side fact, not a UI state.
type Admin struct {
	ID uint `gorm:"primaryKey"`

	Email        string `gorm:"column:email;not null;uniqueIndex;check:email = lower(email)"`
	PasswordHash string `gorm:"column:password_hash;not null"`
	// The check constraint is what makes "three roles" a fact rather than a
	// convention: a typo in a later write path fails at the database instead of
	// silently creating a fourth role the middleware does not know.
	Role Role `gorm:"column:role;not null;type:varchar(16);check:role IN ('owner','support','auditor')"`
	// Deactivating an admin is written with an explicit column update, never a
	// plain Create/Save: GORM drops zero-valued fields on insert, and `false` is a
	// zero value this column must be able to hold.
	IsActive bool `gorm:"column:is_active;not null;default:true"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName pins the plural snake_case table name.
func (Admin) TableName() string { return "admins" }

// Session is one panel session. It mirrors the buyer's hashed-token shape but is
// a separate table, so a bug in one lookup cannot escalate into the other
// (ADR 0024 §2). RevokedAt makes sign-out immediate: the cookie may still arrive,
// the lookup simply stops matching.
type Session struct {
	ID uint `gorm:"primaryKey"`

	// TokenHash is the SHA-256 of the opaque token in the cookie; the raw token
	// is never stored (ADR 0009/0011).
	TokenHash string     `gorm:"column:token_hash;not null;uniqueIndex"`
	AdminID   uint       `gorm:"column:admin_id;not null;index"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null"`
	RevokedAt *time.Time `gorm:"column:revoked_at"`

	CreatedAt time.Time
}

// TableName pins the plural snake_case table name.
func (Session) TableName() string { return "admin_sessions" }

// Action names one audited act. The strings are the vocabulary of the trail, so
// they are stable: a report written today must still match rows written next year.
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

// Payload is the small JSONB document an audit row carries: the before/after of
// the catalog fields it changed, or the Transaction reference for a PIN reveal.
// It holds no secrets — a PIN is never written here (ADR 0027 §4).
type Payload map[string]any

// Value implements driver.Valuer.
func (p Payload) Value() (driver.Value, error) {
	if p == nil {
		return []byte("{}"), nil
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode audit payload: %w", err)
	}
	return encoded, nil
}

// Scan implements sql.Scanner.
func (p *Payload) Scan(value any) error {
	if value == nil {
		*p = Payload{}
		return nil
	}

	var raw []byte
	switch typed := value.(type) {
	case []byte:
		raw = typed
	case string:
		raw = []byte(typed)
	default:
		return fmt.Errorf("scan audit payload: unsupported type %T", value)
	}

	var decoded Payload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("scan audit payload: %w", err)
	}
	*p = decoded
	return nil
}

// AuditEntry is one row of `admin_audit_log`. The table is append-only: there is
// no update and no delete path anywhere in the system, and no panel endpoint that
// could purge it (ADR 0027 §3).
//
// AdminEmail is copied at write time so the row still reads after the admin it
// names is gone; AdminID alone would leave a dangling number.
type AuditEntry struct {
	ID uint `gorm:"primaryKey"`

	AdminID    uint    `gorm:"column:admin_id;not null;index"`
	AdminEmail string  `gorm:"column:admin_email;not null"`
	Action     Action  `gorm:"column:action;not null;index"`
	TargetType string  `gorm:"column:target_type;not null;default:''"`
	TargetID   string  `gorm:"column:target_id;not null;default:''"`
	Payload    Payload `gorm:"column:payload;type:jsonb;not null;default:'{}'"`
	// IP is where the action came from, as the server saw it.
	IP string `gorm:"column:ip;not null;default:''"`

	CreatedAt time.Time
}

// TableName pins the singular table name ADR 0027 named; it is one log, not a
// collection of logs.
func (AuditEntry) TableName() string { return "admin_audit_log" }
