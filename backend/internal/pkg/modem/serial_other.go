//go:build !linux

package modem

import "fmt"

// openTransport is Linux-only: the deployment runs on the host that holds the
// modem, and the serial termios setup is platform specific. Other platforms get a
// clear error instead of a silently broken sender.
func openTransport(settings Settings) (Transport, error) {
	return nil, fmt.Errorf("the SMS modem adapter supports linux only (device %q)", settings.Device)
}
