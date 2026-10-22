// Command migrate applies the Postgres schema and the idempotent Package catalog
// seed, then exits. Deployment runs it once before the API starts (ADR 0012).
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
