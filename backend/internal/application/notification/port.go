// Package notification owns the SMS sender port. The modem adapter (AT commands,
// ADR 0003) implements it later; the Transaction/Provisioning code only ever
// sees "send this text to this Phone Number".
package notification

import "context"

// Sender delivers one SMS. Implementations must be safe for serialized use: only
// the worker process holds the modem.
type Sender interface {
	Send(ctx context.Context, phoneNumber, message string) error
}
