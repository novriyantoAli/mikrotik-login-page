// Package pin owns the two things this system must not improvise: how a PIN is
// generated and how it is stored at rest (ADR 0004/0016).
//
// A PIN is six digits, replaced on every purchase, and kept encrypted in
// Postgres so Account Status can show it again after the SMS is gone. Only the
// ciphertext and the key version are stored; the plaintext exists in memory for
// one provisioning run and in the SMS the buyer receives, never in a log line.
package pin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const (
	// Length is how many digits a PIN has (ADR 0004).
	Length = 6
	// KeyBytes is the AES-256 key size this package accepts.
	KeyBytes = 32
	// nonceBytes is the GCM nonce size; it is prepended to every ciphertext.
	nonceBytes = 12
	// digitSpace is the number of PINs a Length-digit code can hold.
	digitSpace = 1_000_000
)

var (
	// ErrNoKey means the cipher was built without any key material.
	ErrNoKey = errors.New("pin: no encryption key configured")
	// ErrUnknownKeyVersion means a ciphertext names a key this deployment does
	// not configure. It is never retried with the active key: that would turn a
	// configuration fault into an unreadable PIN.
	ErrUnknownKeyVersion = errors.New("pin: unknown encryption key version")
)

// Generate returns a fresh six-digit PIN. crypto/rand keeps it unguessable;
// leading zeros are kept so the value is always exactly Length digits.
func Generate() (string, error) {
	number, err := rand.Int(rand.Reader, big.NewInt(digitSpace))
	if err != nil {
		return "", fmt.Errorf("generate pin: %w", err)
	}
	return fmt.Sprintf("%0*d", Length, number.Int64()), nil
}

// Settings are the deployment's PIN keys, as read from configuration.
type Settings struct {
	// Keys lists "version=base64(32 bytes)" entries separated by commas.
	Keys string
	// ActiveVersion is the version written with every new ciphertext.
	ActiveVersion int
}

// Cipher seals and opens PINs with AES-256-GCM under versioned keys. One
// instance lives for the process; it is safe for concurrent use because
// cipher.AEAD is.
type Cipher struct {
	aeads  map[int]cipher.AEAD
	active int
}

// NewCipher parses the configured keys and rejects a deployment that cannot
// serve: no keys at all, malformed material, or an active version that is not
// configured.
func NewCipher(settings Settings) (*Cipher, error) {
	keys, err := parseKeys(settings.Keys)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, ErrNoKey
	}

	aeads := make(map[int]cipher.AEAD, len(keys))
	for version, material := range keys {
		block, err := aes.NewCipher(material)
		if err != nil {
			return nil, fmt.Errorf("pin: key version %d: %w", version, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("pin: key version %d: %w", version, err)
		}
		aeads[version] = aead
	}

	if _, ok := aeads[settings.ActiveVersion]; !ok {
		return nil, fmt.Errorf("%w: active version %d is not configured",
			ErrUnknownKeyVersion, settings.ActiveVersion)
	}

	return &Cipher{aeads: aeads, active: settings.ActiveVersion}, nil
}

// ActiveVersion returns the key version new ciphertexts carry.
func (c *Cipher) ActiveVersion() int { return c.active }

// Encrypt seals plaintext with the active key and returns nonce||ciphertext,
// which is what the Transaction row stores.
func (c *Cipher) Encrypt(plaintext string) ([]byte, error) {
	aead, ok := c.aeads[c.active]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownKeyVersion, c.active)
	}

	nonce := make([]byte, nonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("pin: generate nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt opens a ciphertext with the key version stored beside it, so an old
// row stays readable after a rotation.
func (c *Cipher) Decrypt(ciphertext []byte, version int) (string, error) {
	aead, ok := c.aeads[version]
	if !ok {
		return "", fmt.Errorf("%w: %d", ErrUnknownKeyVersion, version)
	}
	if len(ciphertext) <= nonceBytes {
		return "", errors.New("pin: ciphertext is too short")
	}

	plaintext, err := aead.Open(nil, ciphertext[:nonceBytes], ciphertext[nonceBytes:], nil)
	if err != nil {
		return "", fmt.Errorf("pin: decrypt: %w", err)
	}
	return string(plaintext), nil
}

// parseKeys reads "version=base64" entries. Duplicates and unusable material are
// errors rather than silent last-wins, because a mistyped key would otherwise
// encrypt PINs nobody can read back.
func parseKeys(raw string) (map[int][]byte, error) {
	keys := make(map[int][]byte)

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		versionPart, materialPart, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("pin: key entry %q must be version=base64", entry)
		}

		version, err := strconv.Atoi(strings.TrimSpace(versionPart))
		if err != nil || version < 1 {
			return nil, fmt.Errorf("pin: key entry %q has no positive version", entry)
		}
		if _, exists := keys[version]; exists {
			return nil, fmt.Errorf("pin: key version %d is configured twice", version)
		}

		material, err := base64.StdEncoding.DecodeString(strings.TrimSpace(materialPart))
		if err != nil {
			return nil, fmt.Errorf("pin: key version %d is not base64: %w", version, err)
		}
		if len(material) != KeyBytes {
			return nil, fmt.Errorf("pin: key version %d must be %d bytes, got %d",
				version, KeyBytes, len(material))
		}
		keys[version] = material
	}

	return keys, nil
}
