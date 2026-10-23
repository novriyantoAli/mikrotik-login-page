// Package config owns every deployment knob of the backend. Values are read
// from config.yaml and overridden by environment variables using the
// SECTION_KEY convention (postgres.host -> POSTGRES_HOST). Nothing that
// differs per deployment may be hardcoded elsewhere: code receives a
// *config.Config through Fx instead of reading the environment itself.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Environment values accepted by AppConfig.Environment.
const (
	EnvironmentDevelopment = "development"
	EnvironmentProduction  = "production"
)

// MaxShutdownTimeout is the largest graceful shutdown the process can honor.
// It must stay below the Fx stop budget in internal/app so a configured timeout
// can never turn a clean SIGTERM into a non-zero exit.
const MaxShutdownTimeout = 25 * time.Second

// Log formats accepted by LoggerConfig.Format.
const (
	LogFormatJSON    = "json"
	LogFormatConsole = "console"
)

// Config is the root configuration object injected through Fx.
type Config struct {
	App             AppConfig      `mapstructure:"app"`
	Server          ServerConfig   `mapstructure:"server"`
	Logger          LoggerConfig   `mapstructure:"logger"`
	Postgres        PostgresConfig `mapstructure:"postgres"`
	FreeRADIUSMySQL MySQLConfig    `mapstructure:"freeradius_mysql"`
	Redis           RedisConfig    `mapstructure:"redis"`
	Worker          WorkerConfig   `mapstructure:"worker"`
	Health          HealthConfig   `mapstructure:"health"`
	Auth            AuthConfig     `mapstructure:"auth"`
	SMS             SMSConfig      `mapstructure:"sms"`
}

// AppConfig identifies this service in logs and health responses.
type AppConfig struct {
	Name        string `mapstructure:"name"`
	Environment string `mapstructure:"environment"`
}

// ServerConfig configures the public HTTP API.
type ServerConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	// TrustedProxies is a comma-separated CIDR list whose X-Forwarded-For
	// header is believed. Empty means "trust nobody": ClientIP() then reports
	// the socket peer. In the compose deployment this is the reverse proxy's
	// network (ADR 0009).
	TrustedProxies string `mapstructure:"trusted_proxies"`
}

// LoggerConfig configures zap.
type LoggerConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	OutputPath string `mapstructure:"output_path"`
}

// PostgresConfig points at the application's own database.
type PostgresConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Database        string        `mapstructure:"database"`
	SSLMode         string        `mapstructure:"ssl_mode"`
	SearchPath      string        `mapstructure:"search_path"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// DSN renders the libpq/pgx connection string for this Postgres instance.
// search_path is empty in normal deployments; tests set it to bind every
// connection to a schema created for that test.
func (c PostgresConfig) DSN() string {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Database, c.SSLMode,
	)
	if c.SearchPath != "" {
		// libpq/pgx keyword/value DSNs need the whole options value quoted.
		dsn += " options='-c search_path=" + c.SearchPath + "'"
	}
	return dsn
}

// TrustedProxyCIDRs splits and trims the configured proxy list.
func (c ServerConfig) TrustedProxyCIDRs() []string {
	if strings.TrimSpace(c.TrustedProxies) == "" {
		return nil
	}

	parts := strings.Split(c.TrustedProxies, ",")
	cidrs := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			cidrs = append(cidrs, trimmed)
		}
	}
	return cidrs
}

// MySQLConfig points at the FreeRADIUS database. It is a separate server from
// Postgres on purpose (ADR 0002): FreeRADIUS keeps its own schema.
type MySQLConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Database        string        `mapstructure:"database"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// DSN renders the go-sql-driver/mysql connection string.
func (c MySQLConfig) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&loc=UTC",
		c.User, c.Password, c.Host, c.Port, c.Database,
	)
}

// RedisConfig configures the Asynq backing store and any cache use.
type RedisConfig struct {
	Addr        string        `mapstructure:"addr"`
	Password    string        `mapstructure:"password"`
	DB          int           `mapstructure:"db"`
	DialTimeout time.Duration `mapstructure:"dial_timeout"`
}

// WorkerConfig configures the Asynq worker server.
type WorkerConfig struct {
	Concurrency int      `mapstructure:"concurrency"`
	Queues      []string `mapstructure:"queues"`
}

// HealthConfig configures the dependency health check.
type HealthConfig struct {
	Timeout time.Duration `mapstructure:"timeout"`
}

// AuthConfig configures the passwordless entry path: Magic Link lifetime,
// session lifetime, the cookie that carries the session, the rate limits on
// requesting a Magic Link, and where the app is reachable when the SMS link is
// built (ADR 0011).
type AuthConfig struct {
	// MagicLinkTTL is how long a Magic Link stays usable.
	MagicLinkTTL time.Duration `mapstructure:"magic_link_ttl"`
	// SessionTTL is the lifetime of the cookie session and of its hash row.
	SessionTTL time.Duration `mapstructure:"session_ttl"`
	// CookieSecure sets the Secure attribute; off only for local plain-HTTP
	// deployments behind the reverse proxy (ADR 0011).
	CookieSecure bool `mapstructure:"cookie_secure"`
	// CookieName is the name of the session cookie.
	CookieName string `mapstructure:"cookie_name"`
	// AppBaseURL is the origin the SMS link points at. It is deployment
	// configuration, never derived from a request header.
	AppBaseURL string `mapstructure:"app_base_url"`
	// MagicLinkPath is the app route that redeems the token.
	MagicLinkPath string `mapstructure:"magic_link_path"`
	// LoginPageAllowlist holds the absolute Login Page URLs a `return` may name.
	// Empty means every return target is dropped (ADR 0011).
	LoginPageAllowlist []string `mapstructure:"login_page_allowlist"`
	// RateLimit bounds Magic Link requests per Phone Number and per IP.
	RateLimit AuthRateLimitConfig `mapstructure:"rate_limit"`
}

// MagicLinkURL renders the link the SMS carries for one raw token.
func (c AuthConfig) MagicLinkURL(token string) string {
	base := strings.TrimSuffix(c.AppBaseURL, "/")
	path := c.MagicLinkPath
	if path == "" || path[0] != '/' {
		path = "/" + path
	}
	return base + path + "?token=" + url.QueryEscape(token)
}

// AuthRateLimitConfig is one fixed window per dimension, both counted in Redis.
type AuthRateLimitConfig struct {
	PhoneWindow time.Duration `mapstructure:"phone_window"`
	PhoneLimit  int           `mapstructure:"phone_limit"`
	IPWindow    time.Duration `mapstructure:"ip_window"`
	IPLimit     int           `mapstructure:"ip_limit"`
}

// SMSConfig configures the modem adapter and the outbox drainer (ADR 0010).
type SMSConfig struct {
	// Device is the serial device the Wavecom modem is attached to.
	Device string `mapstructure:"device"`
	// Baud is the serial speed the modem is configured for.
	Baud int `mapstructure:"baud"`
	// CommandTimeout bounds one AT command round trip.
	CommandTimeout time.Duration `mapstructure:"command_timeout"`
	// Lease is how long a claimed outbox row stays owned by one drainer.
	Lease time.Duration `mapstructure:"lease"`
	// SweepInterval is how often the drainer looks for due rows even when no
	// wake-up task arrived.
	SweepInterval time.Duration `mapstructure:"sweep_interval"`
	// MaxAttempts is the number of send attempts before a row is left failed.
	MaxAttempts int `mapstructure:"max_attempts"`
	// RetryBackoff is the base delay between attempts; it doubles per attempt.
	RetryBackoff time.Duration `mapstructure:"retry_backoff"`
}

// defaults doubles as the documented list of every configuration key. Each key
// is also bound to its environment variable so that viper.Unmarshal sees env
// overrides (AutomaticEnv alone does not populate nested unmarshalled keys).
var defaults = map[string]any{
	"app.name":                           "mikrotik-login-page",
	"app.environment":                    EnvironmentDevelopment,
	"server.host":                        "0.0.0.0",
	"server.port":                        8080,
	"server.read_timeout":                "10s",
	"server.write_timeout":               "15s",
	"server.idle_timeout":                "60s",
	"server.shutdown_timeout":            "15s",
	"server.trusted_proxies":             "",
	"logger.level":                       "info",
	"logger.format":                      LogFormatConsole,
	"logger.output_path":                 "stdout",
	"postgres.host":                      "localhost",
	"postgres.port":                      5432,
	"postgres.user":                      "hotspot",
	"postgres.password":                  "",
	"postgres.database":                  "hotspot",
	"postgres.ssl_mode":                  "disable",
	"postgres.search_path":               "",
	"postgres.max_open_conns":            20,
	"postgres.max_idle_conns":            10,
	"postgres.conn_max_lifetime":         "30m",
	"freeradius_mysql.host":              "localhost",
	"freeradius_mysql.port":              3306,
	"freeradius_mysql.user":              "radius",
	"freeradius_mysql.password":          "",
	"freeradius_mysql.database":          "radius",
	"freeradius_mysql.max_open_conns":    10,
	"freeradius_mysql.max_idle_conns":    5,
	"freeradius_mysql.conn_max_lifetime": "30m",
	"redis.addr":                         "localhost:6379",
	"redis.password":                     "",
	"redis.db":                           0,
	"redis.dial_timeout":                 "5s",
	"worker.concurrency":                 10,
	"worker.queues":                      []string{"critical", "default", "low"},
	"health.timeout":                     "2s",
	"auth.magic_link_ttl":                "15m",
	"auth.session_ttl":                   "720h",
	"auth.cookie_secure":                 false,
	"auth.cookie_name":                   "session",
	"auth.app_base_url":                  "http://localhost:5173",
	"auth.magic_link_path":               "/masuk/tautan",
	"auth.login_page_allowlist":          []string{},
	"auth.rate_limit.phone_window":       "5m",
	"auth.rate_limit.phone_limit":        1,
	"auth.rate_limit.ip_window":          "5m",
	"auth.rate_limit.ip_limit":           10,
	"sms.device":                         "/dev/ttyUSB0",
	"sms.baud":                           115200,
	"sms.command_timeout":                "10s",
	"sms.lease":                          "2m",
	"sms.sweep_interval":                 "30s",
	"sms.max_attempts":                   5,
	"sms.retry_backoff":                  "1m",
}

// Keys returns every configuration key this package understands. Tests and
// .env.example use it to prove that no key is undocumented.
func Keys() []string {
	keys := make([]string, 0, len(defaults))
	for key := range defaults {
		keys = append(keys, key)
	}
	return keys
}

// EnvName maps a configuration key to the environment variable that overrides
// it (postgres.host -> POSTGRES_HOST).
func EnvName(key string) string {
	return strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// Load reads config.yaml (if present) plus environment overrides and validates
// the result.
func Load() (*Config, error) {
	v, err := NewViper()
	if err != nil {
		return nil, err
	}

	cfg, err := unmarshal(v)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// NewViper builds the reader used by Load and by tests. A fresh instance is
// used instead of viper's package-level singleton so parallel tests cannot
// observe each other's overrides.
func NewViper() (*viper.Viper, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	for key, value := range defaults {
		v.SetDefault(key, value)
		if err := v.BindEnv(key); err != nil {
			return nil, fmt.Errorf("bind env for %s: %w", key, err)
		}
	}

	// CONFIG_FILE lets deployments point at a file outside the working
	// directory (used by the container image and by make targets).
	if file := os.Getenv("CONFIG_FILE"); file != "" {
		v.SetConfigFile(file)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/mikrotik-login-page")
	}

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	return v, nil
}

func unmarshal(v *viper.Viper) (*Config, error) {
	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(
		mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		),
	)); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return &cfg, nil
}

// Validate rejects configurations that cannot serve traffic. It never fills in
// values: a missing required key is an error, not a silent default.
func (c *Config) Validate() error {
	var problems []string
	problems = append(problems, c.validateApp()...)
	problems = append(problems, c.validateServer()...)
	problems = append(problems, c.validateLogger()...)
	problems = append(problems, c.validatePostgres()...)
	problems = append(problems, c.validateFreeRADIUS()...)
	problems = append(problems, c.validateRedis()...)
	problems = append(problems, c.validateWorker()...)
	problems = append(problems, c.validateHealth()...)
	problems = append(problems, c.validateAuth()...)
	problems = append(problems, c.validateSMS()...)

	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

func (c *Config) validateApp() []string {
	var problems []string

	if c.App.Name == "" {
		problems = append(problems, "app.name must be set")
	}
	switch c.App.Environment {
	case EnvironmentDevelopment, EnvironmentProduction:
	default:
		problems = append(problems, fmt.Sprintf("app.environment must be %q or %q, got %q",
			EnvironmentDevelopment, EnvironmentProduction, c.App.Environment))
	}
	return problems
}

func (c *Config) validateServer() []string {
	var problems []string

	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		problems = append(problems, fmt.Sprintf("server.port must be between 1 and 65535, got %d", c.Server.Port))
	}
	if c.Server.ReadTimeout <= 0 || c.Server.WriteTimeout <= 0 || c.Server.IdleTimeout <= 0 {
		problems = append(problems, "server read_timeout, write_timeout and idle_timeout must be positive durations")
	}
	if c.Server.ShutdownTimeout <= 0 {
		problems = append(problems, "server.shutdown_timeout must be a positive duration")
	}
	if c.Server.ShutdownTimeout > MaxShutdownTimeout {
		problems = append(problems, fmt.Sprintf("server.shutdown_timeout must not exceed %s", MaxShutdownTimeout))
	}
	for _, cidr := range c.Server.TrustedProxyCIDRs() {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			problems = append(problems, fmt.Sprintf("server.trusted_proxies entry %q is not a CIDR", cidr))
		}
	}
	return problems
}

func (c *Config) validateLogger() []string {
	switch c.Logger.Format {
	case LogFormatJSON, LogFormatConsole:
		return nil
	default:
		return []string{fmt.Sprintf("logger.format must be %q or %q, got %q",
			LogFormatJSON, LogFormatConsole, c.Logger.Format)}
	}
}

func (c *Config) validatePostgres() []string {
	var problems []string

	if c.Postgres.Host == "" || c.Postgres.Database == "" || c.Postgres.User == "" {
		problems = append(problems, "postgres.host, postgres.user and postgres.database must be set")
	}
	if c.Postgres.Port <= 0 {
		problems = append(problems, "postgres.port must be positive")
	}
	return problems
}

func (c *Config) validateFreeRADIUS() []string {
	var problems []string

	if c.FreeRADIUSMySQL.Host == "" || c.FreeRADIUSMySQL.Database == "" || c.FreeRADIUSMySQL.User == "" {
		problems = append(problems,
			"freeradius_mysql.host, freeradius_mysql.user and freeradius_mysql.database must be set")
	}
	if c.FreeRADIUSMySQL.Port <= 0 {
		problems = append(problems, "freeradius_mysql.port must be positive")
	}
	return problems
}

func (c *Config) validateRedis() []string {
	var problems []string

	if c.Redis.Addr == "" {
		problems = append(problems, "redis.addr must be set")
	}
	if c.Redis.DialTimeout <= 0 {
		problems = append(problems, "redis.dial_timeout must be a positive duration")
	}
	return problems
}

func (c *Config) validateWorker() []string {
	var problems []string

	if c.Worker.Concurrency <= 0 {
		problems = append(problems, "worker.concurrency must be positive")
	}
	if len(c.Worker.Queues) == 0 {
		problems = append(problems, "worker.queues must list at least one queue")
	}
	return problems
}

func (c *Config) validateHealth() []string {
	if c.Health.Timeout <= 0 {
		return []string{"health.timeout must be a positive duration"}
	}
	return nil
}

func (c *Config) validateAuth() []string {
	var problems []string

	if c.Auth.MagicLinkTTL <= 0 {
		problems = append(problems, "auth.magic_link_ttl must be a positive duration")
	}
	if c.Auth.SessionTTL <= 0 {
		problems = append(problems, "auth.session_ttl must be a positive duration")
	}
	if c.Auth.CookieName == "" {
		problems = append(problems, "auth.cookie_name must be set")
	}
	if c.Auth.AppBaseURL == "" {
		problems = append(problems, "auth.app_base_url must be set")
	}
	if c.Auth.RateLimit.PhoneWindow <= 0 || c.Auth.RateLimit.PhoneLimit <= 0 {
		problems = append(problems, "auth.rate_limit.phone_window and auth.rate_limit.phone_limit must be positive")
	}
	if c.Auth.RateLimit.IPWindow <= 0 || c.Auth.RateLimit.IPLimit <= 0 {
		problems = append(problems, "auth.rate_limit.ip_window and auth.rate_limit.ip_limit must be positive")
	}
	for _, allowed := range c.Auth.LoginPageAllowlist {
		parsed, err := url.Parse(allowed)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			problems = append(problems,
				fmt.Sprintf("auth.login_page_allowlist entry %q must be an absolute URL", allowed))
		}
	}
	return problems
}

func (c *Config) validateSMS() []string {
	var problems []string

	if c.SMS.Device == "" {
		problems = append(problems, "sms.device must be set")
	}
	if c.SMS.Baud <= 0 {
		problems = append(problems, "sms.baud must be positive")
	}
	if c.SMS.CommandTimeout <= 0 || c.SMS.Lease <= 0 || c.SMS.SweepInterval <= 0 {
		problems = append(problems, "sms.command_timeout, sms.lease and sms.sweep_interval must be positive durations")
	}
	if c.SMS.MaxAttempts <= 0 {
		problems = append(problems, "sms.max_attempts must be positive")
	}
	if c.SMS.RetryBackoff <= 0 {
		problems = append(problems, "sms.retry_backoff must be a positive duration")
	}
	return problems
}
