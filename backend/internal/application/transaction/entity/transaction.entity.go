// Package entity holds the persisted shape of a Transaction: one Package
// purchase, its payment state and the instructions the checkout renders. It
// carries gorm tags only; the JSON contract lives in dto (golden rule 6).
package entity

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// State is the Transaction lifecycle from PRD point 15. The strings travel to
// the app, so they are stable and mutually exclusive.
type State string

// Transaction states. `failed` and `expired` are final; a provisioning failure
// stays in `paid` and is retried by #15, never by adding a new state.
const (
	StatePending     State = "pending"
	StatePaid        State = "paid"
	StateProvisioned State = "provisioned"
	StateFailed      State = "failed"
	StateExpired     State = "expired"
)

// allowedTransitions is the one table that decides which state changes exist.
// Everything else (repository SQL, handler, app) asks it instead of restating
// the rules.
var allowedTransitions = map[State][]State{
	StatePending: {StatePaid, StateFailed, StateExpired},
	StatePaid:    {StateProvisioned},
}

// CanTransitionTo reports whether next is reachable from this state. There is no
// backwards transition: `paid → expired` and `expired → paid` are both refused,
// and a repeated callback learns that from the guard instead of a rule written
// again in the service.
func (s State) CanTransitionTo(next State) bool {
	for _, allowed := range allowedTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// Final reports whether the state is an end state (nothing follows it).
func (s State) Final() bool {
	return s == StateProvisioned || s == StateFailed || s == StateExpired
}

// Instruction is one rendering block of the payment instructions ("Cara
// pembayaran", then numbered steps). It is vendor-neutral: only the adapter knows
// where those strings came from.
type Instruction struct {
	Title string   `json:"title"`
	Steps []string `json:"steps"`
}

// Instructions is the JSON list persisted with the Transaction, so a buyer who
// returns to an old checkout sees the same instructions without calling the
// gateway again.
type Instructions []Instruction

// Value implements driver.Valuer.
func (i Instructions) Value() (driver.Value, error) {
	encoded, err := json.Marshal(i)
	if err != nil {
		return nil, fmt.Errorf("encode instructions: %w", err)
	}
	return encoded, nil
}

// Scan implements sql.Scanner.
func (i *Instructions) Scan(value any) error {
	if value == nil {
		*i = Instructions{}
		return nil
	}

	var raw []byte
	switch typed := value.(type) {
	case []byte:
		raw = typed
	case string:
		raw = []byte(typed)
	default:
		return fmt.Errorf("scan instructions: unsupported type %T", value)
	}

	var decoded Instructions
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("scan instructions: %w", err)
	}
	*i = decoded
	return nil
}

// Transaction is one Package purchase. Reference is this system's identity (the
// gateway echoes it as merchant_ref); the vendor's own reference is never stored.
//
// The unique index on (phone_number, package_id) filtered to state='pending' is
// what makes checkout idempotent under concurrency: two "Bayar" taps can produce
// two gateway bills, but only one pending Transaction (PRD point 17).
type Transaction struct {
	ID uint `gorm:"primaryKey"`

	Reference string `gorm:"column:reference;not null;uniqueIndex"`
	// PhoneNumber is canonical ("628…") and is both the buyer identity and the
	// future FreeRADIUS username (CONTEXT.md).
	PhoneNumber string `gorm:"column:phone_number;not null;uniqueIndex:idx_txn_pending,where:state='pending',priority:1"`
	PackageID   uint   `gorm:"column:package_id;not null;uniqueIndex:idx_txn_pending,where:state='pending',priority:2"`
	// PackageName and AmountIDR are snapshotted at checkout: the amount the buyer
	// was shown must not change if the catalog is edited later.
	PackageName string `gorm:"column:package_name;not null"`
	AmountIDR   int64  `gorm:"column:amount_idr;not null"`

	Channel     string `gorm:"column:channel;not null"`
	ChannelName string `gorm:"column:channel_name;not null;default:''"`

	State State `gorm:"column:state;not null;index"`

	PayCode  string `gorm:"column:pay_code;not null;default:''"`
	PayURL   string `gorm:"column:pay_url;not null;default:''"`
	QRString string `gorm:"column:qr_string;not null;default:''"`
	QRURL    string `gorm:"column:qr_url;not null;default:''"`

	Instructions Instructions `gorm:"column:instructions;type:jsonb;not null;default:'[]'"`

	// ReturnURL is the Login Page this checkout came from, already validated
	// against the deployment allowlist. It is echoed to the app so the app never
	// redirects using an unchecked URL from its own address bar (PRD point 27).
	ReturnURL string `gorm:"column:return_url;not null;default:''"`

	ExpiresAt time.Time  `gorm:"column:expires_at;not null"`
	PaidAt    *time.Time `gorm:"column:paid_at"`

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// TableName pins the plural snake_case table name.
func (Transaction) TableName() string { return "transactions" }
