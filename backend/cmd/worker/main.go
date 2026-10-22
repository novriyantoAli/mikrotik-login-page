// Command worker processes background jobs (Asynq) and, later, owns the SMS
// modem. It runs from the same image as the API with a different command.
package main

import (
	"fmt"
	"os"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/app"
)

func main() {
	if err := app.Run(app.Worker()); err != nil {
		fmt.Fprintf(os.Stderr, "worker: %v\n", err)
		os.Exit(1)
	}
}
