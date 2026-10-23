// Package entity holds the persisted shape of the SMS outbox: one row per
// message this system still owes a Phone Number (ADR 0010). Entities carry gorm
// tags only; the JSON contract lives in dto.
package entity

import "time"

// State is the outbox row's lifecycle. A row is written as pending, claimed as
// sending (with a lease), and ends as sent or failed.
type State string

// Outbox states. Keep the strings stable: they are read by operators in psql
// and, later, by the metrics the operator dashboard shows.
const (
	StatePending State = "pending"
	StateSending State = "sending"
	StateSent    State = "sent"
	StateFailed  State = "failed"
)

// SMS is one queued message. Attempts counts claims, not successful sends, so a
// row left failed records how many times the modem was tried.
type SMS struct {
	ID uint `gorm:"primaryKey"`

	PhoneNumber string `gorm:"column:phone_number;not null;index"`
	Message     string `gorm:"column:message;not null"`

	State    State `gorm:"column:state;not null;default:'pending';index"`
	Attempts int   `gorm:"column:attempts;not null;default:0"`
	// NextAttemptAt is when the row becomes claimable again after a failure.
	NextAttemptAt time.Time `gorm:"column:next_attempt_at;not null;index"`
	// LeaseUntil marks the row as owned by one drainer until the lease expires;
	// an expired lease returns the row to pending (ADR 0010).
	LeaseUntil *time.Time `gorm:"column:lease_until"`
	LastError  string     `gorm:"column:last_error;not null;default:''"`
	SentAt     *time.Time `gorm:"column:sent_at"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName pins the plural snake_case table name.
func (SMS) TableName() string { return "sms_outbox" }
