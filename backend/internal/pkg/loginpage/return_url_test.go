package loginpage_test

import (
	"testing"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/loginpage"
)

var allowlist = []string{
	"https://hotspot.local/login",
	"http://192.168.88.1/login",
}

func TestAllowed_AcceptsAnAllowlistedLoginPage(t *testing.T) {
	t.Parallel()

	got, ok := loginpage.Allowed("https://hotspot.local/login?mac=AA:BB&ip=10.0.0.2", allowlist)
	if !ok {
		t.Fatal("expected the allowlisted Login Page to be accepted")
	}
	if got != "https://hotspot.local/login?mac=AA:BB&ip=10.0.0.2" {
		t.Fatalf("return target = %q", got)
	}
}

func TestAllowed_AcceptsTheSameHostOnAnotherScheme(t *testing.T) {
	t.Parallel()

	if _, ok := loginpage.Allowed("http://192.168.88.1/login?mac=AA", allowlist); !ok {
		t.Fatal("expected the second allowlist entry to match")
	}
}

func TestAllowed_AcceptsAPathBelowTheAllowlistedPage(t *testing.T) {
	t.Parallel()

	if _, ok := loginpage.Allowed("https://hotspot.local/login/status", allowlist); !ok {
		t.Fatal("a page below the allowlisted path is still the Login Page")
	}
}

func TestAllowed_RejectsEverythingOutsideTheAllowlist(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"another host":         "https://evil.example/login",
		"same host, no path":   "https://hotspot.local/",
		"path outside":         "https://hotspot.local/account",
		"relative url":         "/login?mac=AA",
		"empty":                "",
		"not a url":            "hotspot.local/login",
		"javascript scheme":    "javascript:alert(1)//https://hotspot.local/login",
		"host suffix attack":   "https://hotspot.local.evil.example/login",
		"subdomain attack":     "https://evil.hotspot.local/login",
		"scheme mismatch":      "ftp://hotspot.local/login",
		"port not allowlisted": "https://hotspot.local:8443/login",
	}
	for name, candidate := range cases {
		if _, ok := loginpage.Allowed(candidate, allowlist); ok {
			t.Errorf("%s: %q was accepted", name, candidate)
		}
	}
}

func TestAllowed_EmptyAllowlistDropsEveryTarget(t *testing.T) {
	t.Parallel()

	if _, ok := loginpage.Allowed("https://hotspot.local/login", nil); ok {
		t.Fatal("an empty allowlist must not accept anything")
	}
}

func TestAllowed_DropsTheFragment(t *testing.T) {
	t.Parallel()

	got, ok := loginpage.Allowed("https://hotspot.local/login#/beli", allowlist)
	if !ok {
		t.Fatal("expected the Login Page to be accepted")
	}
	if got != "https://hotspot.local/login" {
		t.Fatalf("fragment survived: %q", got)
	}
}
