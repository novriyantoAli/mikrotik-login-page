package testutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// Dependencies are the real servers integration tests run against. Postgres and
// Redis are started automatically when no external instance is configured;
// FreeRADIUS's MySQL is external only, because a clean machine is not expected
// to have a MySQL server.
type Dependencies struct {
	PostgresDSN        string
	RedisAddr          string
	FreeRADIUSMySQLDSN string

	stopPostgres func() error
	stopRedis    func() error
}

var (
	dependenciesMu sync.Mutex
	dependencies   *Dependencies
)

// RunWithDependencies starts the real dependencies for a test binary, installs
// them for the harness and stops them afterwards. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.RunWithDependencies(m)) }
func RunWithDependencies(m *testing.M) int {
	deps, err := StartDependencies()
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration dependencies: %v\n", err)
		return 1
	}
	defer deps.Stop()

	dependenciesMu.Lock()
	dependencies = deps
	dependenciesMu.Unlock()

	return m.Run()
}

// StartDependencies resolves each dependency from the environment, starting a
// real Postgres and Redis when TEST_POSTGRES_DSN / TEST_REDIS_ADDR are unset.
func StartDependencies() (*Dependencies, error) {
	deps := &Dependencies{
		PostgresDSN:        os.Getenv("TEST_POSTGRES_DSN"),
		RedisAddr:          os.Getenv("TEST_REDIS_ADDR"),
		FreeRADIUSMySQLDSN: os.Getenv("TEST_FREERADIUS_MYSQL_DSN"),
	}

	if deps.PostgresDSN == "" {
		dsn, stop, err := startEmbeddedPostgres()
		if err != nil {
			return nil, err
		}
		deps.PostgresDSN, deps.stopPostgres = dsn, stop
	}

	if deps.RedisAddr == "" {
		addr, stop, err := startRedis()
		if err != nil {
			deps.Stop()
			return nil, err
		}
		deps.RedisAddr, deps.stopRedis = addr, stop
	}

	return deps, nil
}

// Stop shuts every started dependency down; safe to call more than once.
func (d *Dependencies) Stop() {
	if d.stopRedis != nil {
		_ = d.stopRedis()
		d.stopRedis = nil
	}
	if d.stopPostgres != nil {
		_ = d.stopPostgres()
		d.stopPostgres = nil
	}
}

// RequireDependencies returns the dependencies installed by
// RunWithDependencies, failing the test with an actionable message otherwise.
func RequireDependencies(t *testing.T) *Dependencies {
	t.Helper()

	dependenciesMu.Lock()
	defer dependenciesMu.Unlock()
	if dependencies == nil {
		t.Fatal("integration dependencies are not running: call testutil.RunWithDependencies from TestMain")
	}
	return dependencies
}

func startEmbeddedPostgres() (string, func() error, error) {
	port, err := freePort()
	if err != nil {
		return "", nil, err
	}

	workDir, err := os.MkdirTemp("", "mikrotik-login-page-postgres-*")
	if err != nil {
		return "", nil, err
	}

	// Binaries are cached across runs; only the data directory is per run.
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		cacheRoot = os.TempDir()
	}
	binariesPath := filepath.Join(cacheRoot, "mikrotik-login-page", "embedded-postgres")

	pg := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Version(embeddedpostgres.V16).
			Port(uint32(port)). //nolint:gosec // freePort returns a valid 1-65535 port
			Username("hotspot").
			Password("hotspot").
			Database("hotspot").
			RuntimePath(filepath.Join(workDir, "run")).
			DataPath(filepath.Join(workDir, "data")).
			BinariesPath(binariesPath).
			Logger(io.Discard).
			StartTimeout(90 * time.Second),
	)

	if err := pg.Start(); err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, fmt.Errorf("start embedded postgres: %w", err)
	}

	dsn := fmt.Sprintf(
		"host=127.0.0.1 port=%d user=hotspot password=hotspot dbname=hotspot sslmode=disable",
		port,
	)

	stop := func() error {
		err := pg.Stop()
		_ = os.RemoveAll(workDir)
		return err
	}

	return dsn, stop, nil
}

func startRedis() (string, func() error, error) {
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		return "", nil, fmt.Errorf(
			"redis-server not found in PATH: install Redis or set TEST_REDIS_ADDR to a running instance",
		)
	}

	port, err := freePort()
	if err != nil {
		return "", nil, err
	}

	var output bytes.Buffer
	// binary comes from exec.LookPath("redis-server"), not from user input.
	cmd := exec.Command(binary, //nolint:gosec // binary is resolved via exec.LookPath
		"--port", strconv.Itoa(port),
		"--bind", "127.0.0.1",
		"--save", "",
		"--appendonly", "no",
	)
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return "", nil, fmt.Errorf("start redis-server: %w", err)
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := waitForTCP(addr, 15*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return "", nil, fmt.Errorf("redis did not accept connections on %s: %w (%s)", addr, err, output.String())
	}

	stop := func() error {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			return err
		}

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			return cmd.Process.Kill()
		}
	}

	return addr, stop, nil
}

// FreePort reserves a TCP port for a test that starts a real process.
func FreePort(t *testing.T) int {
	t.Helper()

	port, err := freePort()
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	return port
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve port: %w", err)
	}
	defer func() { _ = listener.Close() }()

	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForTCP(addr string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		dialer := net.Dialer{Timeout: 500 * time.Millisecond}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
