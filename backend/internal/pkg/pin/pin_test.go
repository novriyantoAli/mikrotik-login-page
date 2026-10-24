package pin_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/pin"
)

// key renders one usable AES-256 key as the base64 the configuration carries.
func key(fill byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, pin.KeyBytes))
}

func newCipher(t *testing.T, fill byte, version int) *pin.Cipher {
	t.Helper()

	cipher, err := pin.NewCipher(pin.Settings{
		Keys:          "1=" + key(fill),
		ActiveVersion: version,
	})
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return cipher
}

func TestGenerate_AlwaysSixDigits(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for range 500 {
		value, err := pin.Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if len(value) != pin.Length {
			t.Fatalf("pin %q has %d digits, want %d", value, len(value), pin.Length)
		}
		for _, char := range value {
			if char < '0' || char > '9' {
				t.Fatalf("pin %q is not only digits", value)
			}
		}
		seen[value] = true
	}

	if len(seen) < 2 {
		t.Fatal("500 generated PINs produced a single value; the source is not random")
	}
}

func TestCipher_RoundTripsAndNeverRepeatsItself(t *testing.T) {
	t.Parallel()

	cipher := newCipher(t, 0x11, 1)

	first, err := cipher.Encrypt("012345")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := cipher.Encrypt("012345")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two encryptions of the same PIN produced the same bytes; the nonce is reused")
	}

	decrypted, err := cipher.Decrypt(first, 1)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != "012345" {
		t.Fatalf("decrypted %q, want 012345", decrypted)
	}
}

func TestCipher_KeepsOldVersionsReadable(t *testing.T) {
	t.Parallel()

	rotated, err := pin.NewCipher(pin.Settings{
		Keys:          "1=" + key(0x11) + ",2=" + key(0x22),
		ActiveVersion: 2,
	})
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}

	ciphertext, err := rotated.Encrypt("654321")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if rotated.ActiveVersion() != 2 {
		t.Fatalf("ActiveVersion = %d, want 2", rotated.ActiveVersion())
	}

	decrypted, err := rotated.Decrypt(ciphertext, 2)
	if err != nil {
		t.Fatalf("Decrypt with the stored version: %v", err)
	}
	if decrypted != "654321" {
		t.Fatalf("decrypted %q, want 654321", decrypted)
	}

	old := newCipher(t, 0x11, 1)
	if _, err := old.Decrypt(ciphertext, 2); !errors.Is(err, pin.ErrUnknownKeyVersion) {
		t.Fatalf("reading a version this deployment dropped = %v, want ErrUnknownKeyVersion", err)
	}
}

func TestCipher_RejectsTampering(t *testing.T) {
	t.Parallel()

	cipher := newCipher(t, 0x11, 1)
	ciphertext, err := cipher.Encrypt("123456")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	tampered := append([]byte(nil), ciphertext...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := cipher.Decrypt(tampered, 1); err == nil {
		t.Fatal("a modified ciphertext decrypted successfully")
	}

	if _, err := cipher.Decrypt(ciphertext[:3], 1); err == nil {
		t.Fatal("a truncated ciphertext decrypted successfully")
	}

	if _, err := cipher.Decrypt(ciphertext, 7); !errors.Is(err, pin.ErrUnknownKeyVersion) {
		t.Fatalf("unknown version = %v, want ErrUnknownKeyVersion", err)
	}
}

func TestNewCipher_RejectsUnusableSettings(t *testing.T) {
	t.Parallel()

	cases := map[string]pin.Settings{
		"no keys":              {Keys: "", ActiveVersion: 1},
		"no version":           {Keys: key(0x11), ActiveVersion: 1},
		"not base64":           {Keys: "1=not*base64", ActiveVersion: 1},
		"short key":            {Keys: "1=" + base64.StdEncoding.EncodeToString([]byte("short")), ActiveVersion: 1},
		"active not listed":    {Keys: "1=" + key(0x11), ActiveVersion: 2},
		"duplicate version":    {Keys: "1=" + key(0x11) + ",1=" + key(0x22), ActiveVersion: 1},
		"version not a number": {Keys: "one=" + key(0x11), ActiveVersion: 1},
	}

	for name, settings := range cases {
		if _, err := pin.NewCipher(settings); err == nil {
			t.Fatalf("%s: NewCipher accepted an unusable configuration", name)
		}
	}
}

func TestNewCipher_AcceptsSpacesBetweenEntries(t *testing.T) {
	t.Parallel()

	cipher, err := pin.NewCipher(pin.Settings{
		Keys:          " 1=" + key(0x11) + " , 2=" + key(0x22) + " ",
		ActiveVersion: 2,
	})
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	if cipher.ActiveVersion() != 2 {
		t.Fatalf("ActiveVersion = %d, want 2", cipher.ActiveVersion())
	}
}
