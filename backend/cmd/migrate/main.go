// Command migrate applies the Postgres schema, the idempotent Package catalog
// seed and the first admin owner, then exits. Deployment runs it once before the
// API starts (ADR 0012, ADR 0024).
package main

import (
	"fmt"
	"os"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/app"
)

func main() {
	if err := app.RunOnce(app.Migrate()); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}
