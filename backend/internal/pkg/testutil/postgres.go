package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// NewPostgresSchema creates a throwaway schema for one test and drops it when
// the test ends. Every test therefore starts from an empty database without
// paying for a new server.
func NewPostgresSchema(t *testing.T, dsn string) string {
	t.Helper()

	admin := openGorm(t, dsn)
	schema := "test_" + randomSuffix()

	if err := admin.Exec(`CREATE SCHEMA ` + quoteIdentifier(schema)).Error; err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}

	t.Cleanup(func() {
		if err := admin.Exec(`DROP SCHEMA ` + quoteIdentifier(schema) + ` CASCADE`).Error; err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	return schema
}

// OpenPostgres opens a GORM handle bound to one schema, for tests that need to
// inspect or seed rows directly.
func OpenPostgres(t *testing.T, dsn, schema string) *gorm.DB {
	t.Helper()
	return openGorm(t, withSearchPath(dsn, schema))
}

// PostgresEnv renders the POSTGRES_* overrides that point the application at
// one schema of the test server.
func PostgresEnv(t *testing.T, dsn, schema string) map[string]string {
	t.Helper()

	settings, err := parsePostgresDSN(dsn)
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}

	return map[string]string{
		"POSTGRES_HOST":        settings["host"],
		"POSTGRES_PORT":        settings["port"],
		"POSTGRES_USER":        settings["user"],
		"POSTGRES_PASSWORD":    settings["password"],
		"POSTGRES_DATABASE":    settings["dbname"],
		"POSTGRES_SSL_MODE":    settings["sslmode"],
		"POSTGRES_SEARCH_PATH": schema,
	}
}

// ApplyPostgresEnv points the application at one schema of the test server.
func ApplyPostgresEnv(t *testing.T, dsn, schema string) {
	t.Helper()
	applyEnv(t, PostgresEnv(t, dsn, schema))
}

// RedisEnv renders the REDIS_* overrides for the test Redis instance.
func RedisEnv(t *testing.T, addr string) map[string]string {
	t.Helper()

	host, port, err := splitHostPort(addr)
	if err != nil {
		t.Fatalf("parse redis addr: %v", err)
	}

	return map[string]string{
		"REDIS_ADDR":     host + ":" + port,
		"REDIS_PASSWORD": "",
		"REDIS_DB":       "0",
	}
}

// ApplyRedisEnv points the application at the test Redis instance.
func ApplyRedisEnv(t *testing.T, addr string) {
	t.Helper()
	applyEnv(t, RedisEnv(t, addr))
}

// MySQLEnv renders the FREERADIUS_MYSQL_* overrides. An empty dsn points at a
// closed port, so the health endpoint reports that dependency as down instead
// of the test skipping the case.
func MySQLEnv(t *testing.T, dsn string) map[string]string {
	t.Helper()

	if dsn == "" {
		return map[string]string{
			"FREERADIUS_MYSQL_HOST":     "127.0.0.1",
			"FREERADIUS_MYSQL_PORT":     "1",
			"FREERADIUS_MYSQL_USER":     "radius",
			"FREERADIUS_MYSQL_PASSWORD": "radius",
			"FREERADIUS_MYSQL_DATABASE": "radius",
		}
	}

	settings, err := parseMySQLDSN(dsn)
	if err != nil {
		t.Fatalf("parse freeradius mysql dsn: %v", err)
	}

	return map[string]string{
		"FREERADIUS_MYSQL_HOST":     settings["host"],
		"FREERADIUS_MYSQL_PORT":     settings["port"],
		"FREERADIUS_MYSQL_USER":     settings["user"],
		"FREERADIUS_MYSQL_PASSWORD": settings["password"],
		"FREERADIUS_MYSQL_DATABASE": settings["dbname"],
	}
}

// ApplyMySQLEnv points the application at a FreeRADIUS MySQL instance.
func ApplyMySQLEnv(t *testing.T, dsn string) {
	t.Helper()
	applyEnv(t, MySQLEnv(t, dsn))
}

func applyEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for key, value := range values {
		t.Setenv(key, value)
	}
}

func openGorm(t *testing.T, dsn string) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	return db
}

// withSearchPath binds every pooled connection to one schema.
func withSearchPath(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return dsn
		}
		query := parsed.Query()
		query.Set("options", "-c search_path="+schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	// libpq/pgx keyword/value DSNs need the whole options value quoted.
	return dsn + " options='-c search_path=" + schema + "'"
}

func parsePostgresDSN(dsn string) (map[string]string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return nil, err
		}

		password, _ := parsed.User.Password()
		port := parsed.Port()
		if port == "" {
			port = "5432"
		}
		sslMode := parsed.Query().Get("sslmode")
		if sslMode == "" {
			sslMode = "disable"
		}

		return map[string]string{
			"host":     parsed.Hostname(),
			"port":     port,
			"user":     parsed.User.Username(),
			"password": password,
			"dbname":   strings.TrimPrefix(parsed.Path, "/"),
			"sslmode":  sslMode,
		}, nil
	}

	settings := make(map[string]string)
	for _, field := range strings.Fields(dsn) {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		settings[key] = value
	}

	if settings["sslmode"] == "" {
		settings["sslmode"] = "disable"
	}
	return settings, nil
}

var mysqlDSNPattern = regexp.MustCompile(
	`^(?:(?P<user>[^:@/]*)(?::(?P<password>[^@]*))?@)?tcp\((?P<host>[^:]+):(?P<port>\d+)\)/(?P<dbname>[^?]*)`,
)

func parseMySQLDSN(dsn string) (map[string]string, error) {
	match := mysqlDSNPattern.FindStringSubmatch(dsn)
	if match == nil {
		return nil, fmt.Errorf("unsupported mysql dsn %q: expected user:password@tcp(host:port)/dbname", dsn)
	}

	settings := make(map[string]string, 5)
	for index, name := range mysqlDSNPattern.SubexpNames() {
		if index == 0 || name == "" {
			continue
		}
		settings[name] = match[index]
	}
	return settings, nil
}

func splitHostPort(addr string) (string, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", fmt.Errorf("invalid port %q: %w", port, err)
	}
	return host, port, nil
}

func randomSuffix() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
