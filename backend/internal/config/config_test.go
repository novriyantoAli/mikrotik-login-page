package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

func TestLoadAppliesDefaults(t *testing.T) {
	loadableEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.App.Name != "mikrotik-login-page" {
		t.Fatalf("app.name = %q", cfg.App.Name)
	}
	if cfg.Server.Port != 8080 {
		t.Fatalf("server.port = %d", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 10*time.Second {
		t.Fatalf("server.read_timeout = %v", cfg.Server.ReadTimeout)
	}
	if cfg.Health.Timeout != 2*time.Second {
		t.Fatalf("health.timeout = %v", cfg.Health.Timeout)
	}
	if len(cfg.Worker.Queues) != 3 {
		t.Fatalf("worker.queues = %v", cfg.Worker.Queues)
	}
}

func TestLoadAppliesEnvironmentOverrides(t *testing.T) {
	loadableEnv(t)
	t.Setenv("POSTGRES_HOST", "db.internal")
	t.Setenv("POSTGRES_PORT", "6543")
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("HEALTH_TIMEOUT", "750ms")
	t.Setenv("WORKER_QUEUES", "critical,low")
	t.Setenv("SERVER_TRUSTED_PROXIES", "10.0.0.0/8,192.168.0.0/16")
	t.Setenv("FREE_RADIUS_UNUSED", "ignored")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Postgres.Host != "db.internal" {
		t.Fatalf("postgres.host = %q", cfg.Postgres.Host)
	}
	if cfg.Postgres.Port != 6543 {
		t.Fatalf("postgres.port = %d", cfg.Postgres.Port)
	}
	if cfg.Server.Port != 9090 {
		t.Fatalf("server.port = %d", cfg.Server.Port)
	}
	if cfg.Health.Timeout != 750*time.Millisecond {
		t.Fatalf("health.timeout = %v", cfg.Health.Timeout)
	}
	if strings.Join(cfg.Worker.Queues, ",") != "critical,low" {
		t.Fatalf("worker.queues = %v", cfg.Worker.Queues)
	}
	if strings.Join(cfg.Server.TrustedProxyCIDRs(), ",") != "10.0.0.0/8,192.168.0.0/16" {
		t.Fatalf("server.trusted_proxies = %v", cfg.Server.TrustedProxyCIDRs())
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	loadableEnv(t)
	t.Setenv("SERVER_PORT", "0")
	t.Setenv("LOGGER_FORMAT", "logfmt")
	t.Setenv("SERVER_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("SERVER_TRUSTED_PROXIES", "not-a-cidr")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected validation error")
	}

	for _, expected := range []string{
		"server.port",
		"server.shutdown_timeout",
		"server.trusted_proxies",
		"logger.format",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not mention %q", err, expected)
		}
	}
}

func TestValidateRejectsAnEmptyConfiguration(t *testing.T) {
	cfg := &config.Config{}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}

	for _, expected := range []string{
		"app.name",
		"postgres.host",
		"freeradius_mysql.host",
		"redis.addr",
		"worker.queues",
		"health.timeout",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not mention %q", err, expected)
		}
	}
}

func TestEveryKeyHasAnEnvironmentName(t *testing.T) {
	keys := config.Keys()
	if len(keys) == 0 {
		t.Fatal("no configuration keys")
	}

	for _, key := range keys {
		envName := config.EnvName(key)
		if envName != strings.ToUpper(envName) || strings.Contains(envName, ".") {
			t.Fatalf("key %q maps to unusable env name %q", key, envName)
		}
	}
}

// loadableEnv neutralises anything the developer's shell may export, so the
// test suite is not affected by an ambient deployment environment.
func loadableEnv(t *testing.T) {
	t.Helper()

	for _, key := range config.Keys() {
		t.Setenv(config.EnvName(key), "")
	}
}
