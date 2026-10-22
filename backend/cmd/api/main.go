// Command api serves the public HTTP API and the dependency health endpoint.
//
//	@title			MikroTik Login Page API
//	@version		0.1.0
//	@description	Backend for the hotspot purchase app and the Login Page read contracts.
//	@BasePath		/api
package main

import (
	"fmt"
	"os"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/app"
)

func main() {
	if err := app.Run(app.API()); err != nil {
		fmt.Fprintf(os.Stderr, "api: %v\n", err)
		os.Exit(1)
	}
}
