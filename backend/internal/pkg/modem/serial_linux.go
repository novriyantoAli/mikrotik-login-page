//go:build linux

package modem

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// baudRates maps the configured speed to the termios constant. Only the speeds a
// Wavecom modem is normally configured for are accepted; anything else is a
// configuration error, not a silent 9600 fallback.
var baudRates = map[int]uint32{
	9600:   unix.B9600,
	19200:  unix.B19200,
	38400:  unix.B38400,
	57600:  unix.B57600,
	115200: unix.B115200,
}

// openTransport opens the serial device in raw mode with the configured speed.
func openTransport(settings Settings) (Transport, error) {
	speed, ok := baudRates[settings.Baud]
	if !ok {
		return nil, fmt.Errorf("unsupported modem baud rate %d", settings.Baud)
	}

	fd, err := unix.Open(settings.Device, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", settings.Device, err)
	}

	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("read termios for %s: %w", settings.Device, err)
	}

	// Raw mode: no line discipline, no echo, no translation. The modem speaks
	// binary AT frames and every byte must reach us unchanged.
	termios.Iflag = 0
	termios.Oflag = 0
	termios.Lflag = 0
	termios.Cflag = unix.CS8 | unix.CREAD | unix.CLOCAL | speed
	termios.Cc[unix.VMIN] = 1
	termios.Cc[unix.VTIME] = 0
	termios.Ispeed = speed
	termios.Ospeed = speed

	if err := unix.IoctlSetTermios(fd, unix.TCSETS, termios); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("configure termios for %s: %w", settings.Device, err)
	}

	return &serialTransport{fd: fd}, nil
}

// serialTransport is one open serial port.
type serialTransport struct {
	fd       int
	deadline time.Time
}

func (t *serialTransport) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		n, err := unix.Write(t.fd, p[written:])
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return written, err
		}
		written += n
	}
	return written, nil
}

func (t *serialTransport) Read(p []byte) (int, error) {
	if !t.deadline.IsZero() {
		remaining := time.Until(t.deadline)
		if remaining <= 0 {
			return 0, os.ErrDeadlineExceeded
		}
		//nolint:gosec // fd comes from unix.Open, so it is a small non-negative descriptor
		pollFDs := []unix.PollFd{{Fd: int32(t.fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(pollFDs, int(remaining.Milliseconds())+1)
		if err != nil && err != unix.EINTR {
			return 0, err
		}
		if ready == 0 {
			return 0, os.ErrDeadlineExceeded
		}
	}

	for {
		n, err := unix.Read(t.fd, p)
		if err == unix.EINTR {
			continue
		}
		return n, err
	}
}

func (t *serialTransport) SetReadDeadline(deadline time.Time) error {
	t.deadline = deadline
	return nil
}

func (t *serialTransport) Close() error {
	return unix.Close(t.fd)
}
