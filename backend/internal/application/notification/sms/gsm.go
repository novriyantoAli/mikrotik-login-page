package sms

import (
	"errors"
	"fmt"
)

// MaxSeptets is the payload of one SMS in the GSM 7-bit default alphabet
// (3GPP TS 23.038). The modem adapter speaks text mode (AT+CMGF=1), which has no
// concatenation: a longer body is answered with a bare ERROR by the device, so
// the length has to be a rule here rather than a surprise at the serial port.
const MaxSeptets = 160

// Reasons a body can never be delivered, whatever the modem's mood.
var (
	// ErrTooLong reports a body that needs concatenation, which text mode cannot do.
	ErrTooLong = errors.New("sms text is longer than one GSM-7 SMS")
	// ErrUnencodable reports a character outside the GSM 7-bit alphabet (an emoji,
	// a typographic quote). Sending it means a question mark, a garbled character
	// or an ERROR, depending on the firmware.
	ErrUnencodable = errors.New("sms text has a character outside the GSM-7 alphabet")
)

// Unsendable marks a body no retry can fix. The drainer records it as final
// instead of spending the attempt budget on a guaranteed failure. It carries the
// reason and the length, never the text itself: the body holds a PIN or a Magic
// Link token (ADR 0016).
type Unsendable struct {
	Reason  error
	Septets int
}

func (e *Unsendable) Error() string {
	if e.Reason == ErrTooLong {
		return fmt.Sprintf("%s (%d septets, limit %d)", e.Reason, e.Septets, MaxSeptets)
	}
	return e.Reason.Error()
}

// Unwrap exposes the reason, so errors.Is(err, sms.ErrTooLong) works.
func (e *Unsendable) Unwrap() error { return e.Reason }

// IsUnsendable reports whether err means "this body will fail on every attempt".
func IsUnsendable(err error) bool {
	var unsendable *Unsendable
	return errors.As(err, &unsendable)
}

// CheckText reports whether text can leave as one SMS. A nil error means the
// modem has a chance to accept it; anything else is an Unsendable cause.
func CheckText(text string) error {
	count, encodable := Septets(text)
	if !encodable {
		return &Unsendable{Reason: ErrUnencodable}
	}
	if count > MaxSeptets {
		return &Unsendable{Reason: ErrTooLong, Septets: count}
	}
	return nil
}

// Septets counts how many septets text occupies in the GSM 7-bit alphabet and
// reports whether every character fits it. Characters in the extension table
// cost two septets, which is why a 160-character body is not automatically a
// 160-septet body. The count is only meaningful when ok is true.
func Septets(text string) (int, bool) {
	total := 0
	for _, r := range text {
		cost, ok := gsmCost[r]
		if !ok {
			return 0, false
		}
		total += cost
	}
	return total, true
}

// gsmAlphabet is the GSM 7-bit default alphabet in septet order (3GPP TS 23.038
// §6.2.1). Index is the septet value; '\x1b' at 0x1B introduces the extension
// table below instead of being a character of its own.
const gsmAlphabet = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞ\x1bÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?" +
	"¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"

// gsmExtension is the extension table: every character here costs two septets.
const gsmExtension = "\f^{}\\[~]|€"

// escape introduces a GSM extension-table character in the byte stream, so it can
// never be sent as a body character even though it occupies a septet position.
const escape = '\x1b'

// gsmCost maps each sendable character to its septet cost.
var gsmCost = func() map[rune]int {
	cost := make(map[rune]int, len(gsmAlphabet))
	for _, r := range gsmAlphabet {
		if r != escape {
			cost[r] = 1
		}
	}
	for _, r := range gsmExtension {
		cost[r] = 2
	}
	return cost
}()
