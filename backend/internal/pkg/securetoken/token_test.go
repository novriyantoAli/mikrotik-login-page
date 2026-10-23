package securetoken_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/securetoken"
)

func TestNewProducesAURLSafeTokenAndItsHash(t *testing.T) {
	raw, hash, err := securetoken.New()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("token %q is not raw URL-safe base64: %v", raw, err)
	}
	if len(decoded) != securetoken.Size {
		t.Fatalf("token decodes to %d bytes, want %d", len(decoded), securetoken.Size)
	}
	if hash != securetoken.Hash(raw) {
		t.Fatal("hash does not match Hash(raw)")
	}
	if strings.Contains(hash, raw) {
		t.Fatal("hash must not embed the raw token")
	}
}

func TestNewIsUnique(t *testing.T) {
	seen := make(map[string]bool, 64)
	for range 64 {
		raw, _, err := securetoken.New()
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}
		if seen[raw] {
			t.Fatalf("token %q was generated twice", raw)
		}
		seen[raw] = true
	}
}

func TestHashIsStableAndNotTheInput(t *testing.T) {
	const raw = "the-same-token"

	first, second := securetoken.Hash(raw), securetoken.Hash(raw)
	if first != second {
		t.Fatalf("hash is not stable: %q != %q", first, second)
	}
	if first == raw || strings.Contains(first, raw) {
		t.Fatal("hash must not be the token itself")
	}
	if len(first) != 64 {
		t.Fatalf("SHA-256 hex hash is %d characters, want 64", len(first))
	}
}
