// Package buildinfo exposes values stamped into the binary at build time.
package buildinfo

// Overridable with: go build -ldflags "-X <pkg>.Version=... -X <pkg>.Commit=...".
var (
	Version = "dev"
	Commit  = "unknown"
)
