package sms_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
)

func TestSeptetsCountsTheGSMAlphabet(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
		ok   bool
	}{
		{name: "ascii", text: "PIN: 123456", want: 11, ok: true},
		{name: "empty", text: "", want: 0, ok: true},
		{name: "newline", text: "a\nb", want: 3, ok: true},
		{name: "rupee is a basic character", text: "£", want: 1, ok: true},
		{name: "euro costs two septets", text: "€", want: 2, ok: true},
		{name: "braces cost two septets", text: "{}", want: 4, ok: true},
		{name: "emoji is outside the alphabet", text: "\U0001F680", want: 0, ok: false},
		{name: "typographic quote is outside the alphabet", text: "\u201c", want: 0, ok: false},
		{name: "escape introducer is not a body character", text: "\x1b", want: 0, ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := sms.Septets(test.text)
			if ok != test.ok || got != test.want {
				t.Fatalf("Septets(%q) = (%d, %v), want (%d, %v)",
					test.text, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestCheckTextAcceptsOneFullSegment(t *testing.T) {
	if err := sms.CheckText(strings.Repeat("a", sms.MaxSeptets)); err != nil {
		t.Fatalf("a %d-septet body must fit: %v", sms.MaxSeptets, err)
	}

	// The extension table makes a body too long before it reaches 160 characters.
	body := strings.Repeat("€", sms.MaxSeptets/2) + "a"
	if err := sms.CheckText(body); !errors.Is(err, sms.ErrTooLong) {
		t.Fatalf("CheckText = %v, want ErrTooLong for %d chars of two-septet runes",
			err, len([]rune(body)))
	}
}

func TestCheckTextRejectsOneCharacterTooMany(t *testing.T) {
	err := sms.CheckText(strings.Repeat("a", sms.MaxSeptets+1))
	if !errors.Is(err, sms.ErrTooLong) {
		t.Fatalf("CheckText = %v, want ErrTooLong", err)
	}
	if !sms.IsUnsendable(err) {
		t.Fatalf("a too-long body must be unsendable, got %v", err)
	}
	if strings.Contains(err.Error(), "aaaa") {
		t.Fatalf("the error leaked the body: %q", err.Error())
	}
}

func TestCheckTextRejectsAnUnencodableBody(t *testing.T) {
	err := sms.CheckText("PIN: 123456 \U0001F680")
	if !errors.Is(err, sms.ErrUnencodable) {
		t.Fatalf("CheckText = %v, want ErrUnencodable", err)
	}
	if !sms.IsUnsendable(err) {
		t.Fatalf("an unencodable body must be unsendable, got %v", err)
	}
}

func TestIsUnsendableIgnoresOrdinaryFailures(t *testing.T) {
	if sms.IsUnsendable(errors.New("modem answered ERROR")) {
		t.Fatal("a modem failure is not a permanent body problem")
	}
	if sms.IsUnsendable(nil) {
		t.Fatal("nil is not unsendable")
	}
}
