// Package sms owns the SMS sender port and the wake-up task name. The modem adapter (AT commands,
// ADR 0003) implements it later; the Transaction/Provisioning code only ever
// sees "send this text to this Phone Number".
package sms

import "context"

// Sender delivers one SMS. Implementations must be safe for serialized use: only
// the worker process holds the modem.
type Sender interface {
	Send(ctx context.Context, phoneNumber, message string) error
}
