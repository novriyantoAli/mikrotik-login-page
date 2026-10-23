// Package securetoken produces the random values that stand in for passwords:
// Magic Link tokens and session tokens (ADR 0011). Only the hash is ever stored,
// so a database dump never contains a usable link or a live session.
package securetoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// Size is the number of random bytes behind every token. 256 bits leaves no
// room for guessing, which is why a fast hash is enough (ADR 0011).
const Size = 32

// New returns a fresh raw token (URL-safe, safe to put in an SMS link or a
// cookie) and the hash that belongs in Postgres.
func New() (raw string, hash string, err error) {
	value := make([]byte, Size)
	if _, err := rand.Read(value); err != nil {
		return "", "", fmt.Errorf("generate secure token: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(value)
	return raw, Hash(raw), nil
}

// Hash is the only form of a token this system persists. It is a plain SHA-256
// because the input is 256 uniformly random bits: there is no dictionary to
// attack, and a slow hash would tax every authenticated request (ADR 0011).
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
