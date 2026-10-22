// Package payment owns the PaymentGateway port: the only way the Transaction
// service may talk to a payment provider. No vendor SDK, HTTP client or Tripay
// status code may appear on this interface (ADR 0008); the adapter that
// implements it lives beside this file when the payment domain lands.
package payment

import (
	"context"
	"time"
)

// Status is this system's payment vocabulary, not the vendor's.
type Status string

// Payment lifecycle as seen by the Transaction domain.
const (
	StatusUnpaid  Status = "unpaid"
	StatusPaid    Status = "paid"
	StatusExpired Status = "expired"
	StatusFailed  Status = "failed"
)

// CreateTransactionRequest asks the gateway to open a closed-payment channel.
type CreateTransactionRequest struct {
	// Reference is this system's Transaction reference; the gateway echoes it
	// back on every callback.
	Reference   string
	Amount      int64
	PhoneNumber string
	PackageName string
	Channel     string
	ExpiresAt   time.Time
}

// CreateTransactionResponse carries the instructions the app renders.
type CreateTransactionResponse struct {
	Reference string
	PayCode   string
	PayURL    string
	QRURL     string
	ExpiresAt time.Time
}

// PaymentStatus is a polled view of one payment.
type PaymentStatus struct {
	Reference string
	Status    Status
	Amount    int64
	PaidAt    *time.Time
}

// Callback is a verified settlement notification.
type Callback struct {
	Reference string
	Status    Status
	Amount    int64
	PaidAt    *time.Time
}

// Gateway is the payment vendor boundary.
type Gateway interface {
	CreateTransaction(ctx context.Context, req CreateTransactionRequest) (*CreateTransactionResponse, error)
	GetStatus(ctx context.Context, reference string) (*PaymentStatus, error)
	// VerifyCallback returns an error when the signature does not match, so an
	// unauthenticated caller can never mark a Transaction paid.
	VerifyCallback(raw []byte, signature string) (*Callback, error)
}
