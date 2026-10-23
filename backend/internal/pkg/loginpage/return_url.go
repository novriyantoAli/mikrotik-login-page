// Package loginpage owns the one rule that decides whether a `return` target may
// be sent back to the Login Page. The allowlist is deployment configuration
// (ADR 0011), and both the Magic Link issuer and checkout use this function, so
// the rule cannot drift between the two paths (PRD point 27/71).
package loginpage

import (
	"net/url"
	"strings"
)

// Allowed returns the sanitised return target when candidate matches one of the
// configured Login Page URLs, and ok=false otherwise. A candidate is accepted
// only when its scheme and host match an allowlist entry and its path starts
// with that entry's path: an allowlisted page may not be turned into a redirect
// to another host, and a fragment is dropped because RouterOS never needs it.
func Allowed(candidate string, allowlist []string) (string, bool) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", false
	}

	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}

	for _, allowed := range allowlist {
		target, err := url.Parse(strings.TrimSpace(allowed))
		if err != nil || target.Scheme == "" || target.Host == "" {
			continue
		}
		if !strings.EqualFold(parsed.Scheme, target.Scheme) ||
			!strings.EqualFold(parsed.Host, target.Host) {
			continue
		}
		if !strings.HasPrefix(parsed.Path, target.Path) {
			continue
		}

		parsed.Fragment = ""
		return parsed.String(), true
	}

	return "", false
}
