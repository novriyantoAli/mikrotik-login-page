package config_test

import (
	"encoding/base64"
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
	// A submission is not an AT round trip, so it gets its own budget (ADR 0022).
	if cfg.SMS.SubmitTimeout != time.Minute {
		t.Fatalf("sms.submit_timeout = %v, want the one-minute default", cfg.SMS.SubmitTimeout)
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
	t.Setenv("WORKER_HEALTH_ADDR", ":9099")
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
	// A key without a default is not looked up in the environment at all, so this
	// assertion is what catches the next one (it caught WORKER_HEALTH_ADDR, whose
	// absence left the worker's operator listener disabled in compose).
	if cfg.Worker.HealthAddr != ":9099" {
		t.Fatalf("worker.health_addr = %q, want the WORKER_HEALTH_ADDR override", cfg.Worker.HealthAddr)
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

func TestLoadRejectsASubmissionBudgetLongerThanTheLease(t *testing.T) {
	loadableEnv(t)
	// The row's lease is the contract that only one drainer works on it; a send
	// that outlives the lease breaks that contract (ADR 0010, ADR 0022).
	t.Setenv("SMS_SUBMIT_TIMEOUT", "5m")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "sms.submit_timeout") {
		t.Fatalf("error %q does not mention sms.submit_timeout", err)
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
		"admin.session_ttl",
		"admin.cookie_name",
		"admin.rate_limit.email_window",
		"pin.encryption_keys",
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

// The panel's own identity (ADR 0024): a separate cookie, twelve hours by
// default, and a login budget per IP and per email. No bootstrap credential
// ships as a default, so an untouched checkout cannot boot an owner.
func TestLoadAppliesAdminDefaults(t *testing.T) {
	loadableEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Admin.SessionTTL != 12*time.Hour {
		t.Fatalf("admin.session_ttl = %v, want the twelve-hour default", cfg.Admin.SessionTTL)
	}
	if cfg.Admin.CookieName != "hotspot_admin" {
		t.Fatalf("admin.cookie_name = %q, want its own cookie, not the buyer's", cfg.Admin.CookieName)
	}
	if cfg.Admin.CookieSecure {
		t.Fatal("admin.cookie_secure must default to false; compose turns it on")
	}
	if cfg.Admin.Email != "" || cfg.Admin.Password != "" {
		t.Fatalf("admin bootstrap must have no default, got %q/%q", cfg.Admin.Email, cfg.Admin.Password)
	}
	if cfg.Admin.RateLimit.IPWindow != 5*time.Minute || cfg.Admin.RateLimit.IPLimit != 10 {
		t.Fatalf("admin.rate_limit.ip_* = %v/%d", cfg.Admin.RateLimit.IPWindow, cfg.Admin.RateLimit.IPLimit)
	}
	if cfg.Admin.RateLimit.EmailWindow != 5*time.Minute || cfg.Admin.RateLimit.EmailLimit != 5 {
		t.Fatalf("admin.rate_limit.email_* = %v/%d",
			cfg.Admin.RateLimit.EmailWindow, cfg.Admin.RateLimit.EmailLimit)
	}
}

func TestLoadAppliesAdminEnvironmentOverrides(t *testing.T) {
	loadableEnv(t)
	t.Setenv("ADMIN_EMAIL", "owner@example.com")
	t.Setenv("ADMIN_PASSWORD", "correct horse battery staple")
	t.Setenv("ADMIN_SESSION_TTL", "1h")
	t.Setenv("ADMIN_COOKIE_SECURE", "true")
	t.Setenv("ADMIN_COOKIE_NAME", "panel")
	t.Setenv("ADMIN_RATE_LIMIT_IP_WINDOW", "1m")
	t.Setenv("ADMIN_RATE_LIMIT_IP_LIMIT", "3")
	t.Setenv("ADMIN_RATE_LIMIT_EMAIL_WINDOW", "10m")
	t.Setenv("ADMIN_RATE_LIMIT_EMAIL_LIMIT", "2")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Admin.Email != "owner@example.com" {
		t.Fatalf("admin.email = %q", cfg.Admin.Email)
	}
	if cfg.Admin.Password != "correct horse battery staple" {
		t.Fatalf("admin.password = %q", cfg.Admin.Password)
	}
	if cfg.Admin.SessionTTL != time.Hour {
		t.Fatalf("admin.session_ttl = %v", cfg.Admin.SessionTTL)
	}
	if !cfg.Admin.CookieSecure {
		t.Fatal("admin.cookie_secure = false, want the ADMIN_COOKIE_SECURE override")
	}
	if cfg.Admin.CookieName != "panel" {
		t.Fatalf("admin.cookie_name = %q", cfg.Admin.CookieName)
	}
	if cfg.Admin.RateLimit.IPWindow != time.Minute || cfg.Admin.RateLimit.IPLimit != 3 {
		t.Fatalf("admin.rate_limit.ip_* = %v/%d", cfg.Admin.RateLimit.IPWindow, cfg.Admin.RateLimit.IPLimit)
	}
	if cfg.Admin.RateLimit.EmailWindow != 10*time.Minute || cfg.Admin.RateLimit.EmailLimit != 2 {
		t.Fatalf("admin.rate_limit.email_* = %v/%d",
			cfg.Admin.RateLimit.EmailWindow, cfg.Admin.RateLimit.EmailLimit)
	}
}

// A session with no lifetime and a limiter that never counts are both a door
// with no lock, so startup rejects them (ADR 0024 §2, §4).
func TestLoadRejectsAnUnusableAdminConfiguration(t *testing.T) {
	loadableEnv(t)
	t.Setenv("ADMIN_SESSION_TTL", "0s")
	t.Setenv("ADMIN_RATE_LIMIT_IP_LIMIT", "0")
	t.Setenv("ADMIN_RATE_LIMIT_EMAIL_WINDOW", "0s")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected validation error")
	}

	for _, expected := range []string{
		"admin.session_ttl",
		"admin.rate_limit.ip_limit",
		"admin.rate_limit.email_window",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not mention %q", err, expected)
		}
	}
}

// A shared cookie name would let a buyer sign-in overwrite the operator's
// session, so the two identities may not answer to one cookie (ADR 0024 §2).
func TestLoadRejectsAnAdminCookieSharingTheBuyersName(t *testing.T) {
	loadableEnv(t)
	t.Setenv("ADMIN_COOKIE_NAME", "session")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "admin.cookie_name") {
		t.Fatalf("error %q does not mention admin.cookie_name", err)
	}
}

func TestLoadRejectsAnUnusablePINKey(t *testing.T) {
	loadableEnv(t)
	// Valid base64, wrong length: a deployment that cannot encrypt a PIN must not
	// boot (ADR 0016).
	t.Setenv("PIN_ENCRYPTION_KEYS", "1="+base64.StdEncoding.EncodeToString([]byte("too-short")))

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "pin.encryption_keys") {
		t.Fatalf("error %q does not mention pin.encryption_keys", err)
	}
}

// testPINKey is the 32-byte key these tests load.
const testPINKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// loadableEnv neutralizes anything the developer's shell may export, so the
// test suite is not affected by an ambient deployment environment. The PIN keys
// have no usable default, so they are set to a test key (ADR 0016).
func loadableEnv(t *testing.T) {
	t.Helper()

	for _, key := range config.Keys() {
		t.Setenv(config.EnvName(key), "")
	}
	t.Setenv("PIN_ENCRYPTION_KEYS", "1="+testPINKey)
}
