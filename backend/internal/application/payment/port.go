// Package payment owns the PaymentGateway port: the only way the Transaction
// service may talk to a payment provider. No vendor SDK, HTTP client or Tripay
// status code may appear on this interface (ADR 0008); the adapter that
// implements it lives beside this file when the payment domain lands.
package payment

import (
	"context"
	"errors"
	"time"
)

// ErrSignatureMismatch means a callback did not carry a valid signature over its
// raw payload. The port owns the sentinel so the Transaction service can reject
// an unauthenticated settlement without importing the vendor adapter.
var ErrSignatureMismatch = errors.New("payment: callback signature does not match")

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

// Instruction is one rendering block the checkout shows under the pay code
// ("Cara pembayaran", then numbered steps). It is a plain title/steps pair on
// purpose: the vendor's own wording is data, not a type the domain models.
type Instruction struct {
	Title string
	Steps []string
}

// CreateTransactionResponse carries the instructions the app renders.
type CreateTransactionResponse struct {
	// Reference is this system's Transaction reference, as the gateway echoed it.
	Reference string
	// GatewayReference is the vendor's own id for this payment. It is the only
	// handle a later status read is accepted with: Tripay rejects a detail query
	// by merchant_ref ("Undefined parameter: reference"), so reconciliation must
	// keep this value rather than reusing Reference.
	GatewayReference string
	ChannelName      string
	PayCode          string
	PayURL           string
	QRString         string
	QRURL            string
	Instructions     []Instruction
	ExpiresAt        time.Time
}

// ChannelFee is what the buyer pays on top of the Package price for one channel.
// FlatIDR is integer rupiah; Percent is a rate (0.7 means 0.7%), not money, and
// the checkout displays both separately from the amount (PRD point 19).
type ChannelFee struct {
	FlatIDR int64
	Percent float64
}

// Channel is one closed-payment method the gateway offers. Only the adapter
// knows the vendor's identifiers; everything here is already this system's
// vocabulary.
type Channel struct {
	Code        string
	Name        string
	Group       string
	FeeCustomer ChannelFee
	// MinAmountIDR and MaxAmountIDR are the vendor's bounds on the payment amount.
	// Zero means "the vendor published no bound", not "zero rupiah": a 5.000
	// Package is refused by every virtual account that starts at 10.000, so
	// checkout has to know before it calls the vendor.
	MinAmountIDR int64
	MaxAmountIDR int64
}

// Accepts reports whether amount can be paid through this channel. A channel with
// no published bound accepts everything the other bound allows.
func (c Channel) Accepts(amountIDR int64) bool {
	if c.MinAmountIDR > 0 && amountIDR < c.MinAmountIDR {
		return false
	}
	if c.MaxAmountIDR > 0 && amountIDR > c.MaxAmountIDR {
		return false
	}
	return true
}

// PaymentStatus is a polled view of one payment.
type PaymentStatus struct {
	Reference string
	Status    Status
	Amount    int64
	PaidAt    *time.Time
}

// Callback is a verified settlement notification. Reference is this system's
// Transaction reference, echoed by the gateway, never the vendor's own id.
type Callback struct {
	Reference string
	Status    Status
	Amount    int64
	PaidAt    *time.Time
}

// Gateway is the payment vendor boundary.
type Gateway interface {
	CreateTransaction(ctx context.Context, req CreateTransactionRequest) (*CreateTransactionResponse, error)
	// GetStatus asks about one payment, addressed by the gateway's own reference
	// (CreateTransactionResponse.GatewayReference), never by ours.
	GetStatus(ctx context.Context, gatewayReference string) (*PaymentStatus, error)
	// VerifyCallback returns an error when the signature does not match, so an
	// unauthenticated caller can never mark a Transaction paid.
	VerifyCallback(raw []byte, signature string) (*Callback, error)
	// Channels lists the closed-payment methods checkout may offer, with their
	// fees already in this system's vocabulary.
	Channels(ctx context.Context) ([]Channel, error)
}
