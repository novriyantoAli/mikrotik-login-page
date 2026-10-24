package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/health/dto"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/database"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/pkg/testutil"
)

func backendDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(repoRoot(t), "backend")
	if !fileExists(filepath.Join(dir, "go.mod")) {
		t.Fatalf("backend/go.mod not found in %s", dir)
	}
	return dir
}

// buildBinary builds one command from the same module, so the process tests run
// the real artifact an operator deploys.
func buildBinary(t *testing.T, command string) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), command)
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/"+command)
	cmd.Dir = backendDir(t)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("build %s: %v\n%s", command, err, output.String())
	}
	return binary
}

// processEnv hands the child a clean environment: no ambient config.yaml (the
// working directory is a temp dir) and every deployment value explicit.
func processEnv(t *testing.T, overrides map[string]string) []string {
	t.Helper()

	env := []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
		"TZ=UTC",
		"APP_ENVIRONMENT=development",
		"LOGGER_LEVEL=info",
		"LOGGER_FORMAT=console",
	}
	// The PIN keys are a test fixture, never a real secret (ADR 0016).
	for key, value := range testutil.PINEnv() {
		env = append(env, key+"="+value)
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func mergeEnv(groups ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, group := range groups {
		for key, value := range group {
			merged[key] = value
		}
	}
	return merged
}

// syncBuffer is a goroutine-safe buffer for capturing child process output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestBuiltAPI_ServesHealthAndStopsOnSIGTERM runs the API binary the way the
// container does, reads the health endpoint over TCP, and requires a clean exit
// on SIGTERM.
func TestBuiltAPI_ServesHealthAndStopsOnSIGTERM(t *testing.T) {
	deps := testutil.RequireDependencies(t)
	schema := testutil.NewPostgresSchema(t, deps.PostgresDSN)
	binary := buildBinary(t, "api")
	port := testutil.FreePort(t)

	env := mergeEnv(
		testutil.PostgresEnv(t, deps.PostgresDSN, schema),
		testutil.RedisEnv(t, deps.RedisAddr),
		testutil.MySQLEnv(t, deps.FreeRADIUSMySQLDSN),
		map[string]string{
			"SERVER_HOST": "127.0.0.1",
			"SERVER_PORT": strconv.Itoa(port),
		},
	)

	cmd := exec.Command(binary)
	cmd.Dir = t.TempDir()
	cmd.Env = processEnv(t, env)

	output := &syncBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		t.Fatalf("start api: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})

	health := waitForHealth(t, fmt.Sprintf("http://127.0.0.1:%d/api/health", port), output)

	checks, ok := health["checks"].(map[string]any)
	if !ok {
		t.Fatalf("health checks missing: %v", health)
	}
	postgres, ok := checks[database.CheckPostgres].(map[string]any)
	if !ok || postgres["status"] != dto.CheckUp {
		t.Fatalf("postgres must be up for the built API: %v", checks)
	}
	if _, ok := checks[database.CheckRedis]; !ok {
		t.Fatalf("redis check missing: %v", checks)
	}
	if _, ok := checks[database.CheckFreeRADIUSMySQL]; !ok {
		t.Fatalf("freeradius_mysql check missing: %v", checks)
	}

	stopProcess(t, cmd, output)
}

// TestBuiltWorker_StartsAndStopsOnSIGTERM requires the worker to reach its
// serving state and to shut down cleanly when the orchestrator sends SIGTERM.
func TestBuiltWorker_StartsAndStopsOnSIGTERM(t *testing.T) {
	deps := testutil.RequireDependencies(t)
	binary := buildBinary(t, "worker")

	cmd := exec.Command(binary)
	cmd.Dir = t.TempDir()
	cmd.Env = processEnv(t, testutil.RedisEnv(t, deps.RedisAddr))

	output := &syncBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})

	waitForOutput(t, output, "worker started")
	stopProcess(t, cmd, output)

	if !strings.Contains(output.String(), "worker stopped") {
		t.Fatalf("worker did not log a graceful stop:\n%s", output.String())
	}
}

// TestBuiltWorker_ExitsWhenRedisIsUnreachable proves the worker fails fast
// instead of reporting itself healthy with a dead queue.
func TestBuiltWorker_ExitsWhenRedisIsUnreachable(t *testing.T) {
	binary := buildBinary(t, "worker")
	closedPort := testutil.FreePort(t)

	cmd := exec.Command(binary)
	cmd.Dir = t.TempDir()
	cmd.Env = processEnv(t, map[string]string{
		"REDIS_ADDR":         "127.0.0.1:" + strconv.Itoa(closedPort),
		"REDIS_DIAL_TIMEOUT": "500ms",
	})

	output := &syncBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("worker exited 0 with an unreachable Redis:\n%s", output.String())
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("worker neither started nor failed within 30s:\n%s", output.String())
	}

	if !strings.Contains(output.String(), "worker cannot reach redis") {
		t.Fatalf("worker did not explain the failure:\n%s", output.String())
	}
}

func waitForOutput(t *testing.T, output *syncBuffer, marker string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), marker) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in process output:\n%s", marker, output.String())
}

func waitForHealth(t *testing.T, url string, output *syncBuffer) map[string]any {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)

	var lastErr error
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(100 * time.Millisecond)
			continue
		}

		var envelope struct {
			Data map[string]any `json:"data"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&envelope)
		_ = response.Body.Close()

		// 200 (all dependencies up) and 503 (a dependency is down) both prove
		// the process is serving; the assertion on postgres happens in the test.
		if decodeErr == nil && envelope.Data != nil &&
			(response.StatusCode == http.StatusOK || response.StatusCode == http.StatusServiceUnavailable) {
			return envelope.Data
		}
		lastErr = decodeErr

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("health endpoint %s never answered: %v\n%s", url, lastErr, output.String())
	return nil
}

func stopProcess(t *testing.T, cmd *exec.Cmd, output *syncBuffer) {
	t.Helper()

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("process did not exit cleanly on SIGTERM: %v\n%s", err, output.String())
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("process did not stop within 20s of SIGTERM\n%s", output.String())
	}
}
