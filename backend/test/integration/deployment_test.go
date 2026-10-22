package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
)

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string            `yaml:"image"`
	Command     []string          `yaml:"command"`
	Environment map[string]string `yaml:"environment"`
	Ports       []string          `yaml:"ports"`
}

func loadCompose(t *testing.T) composeFile {
	t.Helper()

	path := filepath.Join(repoRoot(t), "docker-compose.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var parsed composeFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return parsed
}

func readRepoFile(t *testing.T, relativePath string) string {
	t.Helper()

	path := filepath.Join(repoRoot(t), relativePath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// TestComposeServesOneOriginWithEveryService pins the deployment shape from
// ADR 0009: Postgres, MySQL FreeRADIUS, Redis, API, worker, app and one proxy
// that serves the app and forwards /api.
func TestComposeServesOneOriginWithEveryService(t *testing.T) {
	compose := loadCompose(t)

	for _, service := range []string{
		"postgres",
		"freeradius-mysql",
		"redis",
		"api",
		"worker",
		"app",
		"proxy",
	} {
		if _, ok := compose.Services[service]; !ok {
			t.Fatalf("service %q missing from docker-compose.yml", service)
		}
	}

	proxy := compose.Services["proxy"]
	if len(proxy.Ports) == 0 {
		t.Fatal("proxy must publish the single public port")
	}

	proxyConfig := readRepoFile(t, "docker/proxy/nginx.conf")
	if !strings.Contains(proxyConfig, "location /api/") ||
		!strings.Contains(proxyConfig, "proxy_pass http://api:8080") {
		t.Fatalf("proxy config does not forward /api to the API:\n%s", proxyConfig)
	}
	if !strings.Contains(proxyConfig, "proxy_pass http://app:80") {
		t.Fatalf("proxy config does not serve the app:\n%s", proxyConfig)
	}
}

// TestAPIAndWorkerUseOneImageWithDifferentCommands is the acceptance criterion:
// both processes come from the same image, selected by the command.
func TestAPIAndWorkerUseOneImageWithDifferentCommands(t *testing.T) {
	compose := loadCompose(t)

	api := compose.Services["api"]
	worker := compose.Services["worker"]

	if api.Image == "" {
		t.Fatal("api service has no image")
	}
	if api.Image != worker.Image {
		t.Fatalf("api image %q != worker image %q", api.Image, worker.Image)
	}
	if strings.Join(api.Command, " ") != "api" {
		t.Fatalf("api command = %v", api.Command)
	}
	if strings.Join(worker.Command, " ") != "worker" {
		t.Fatalf("worker command = %v", worker.Command)
	}

	dockerfile := readRepoFile(t, "backend/Dockerfile")
	for _, expected := range []string{"./cmd/api", "./cmd/worker", "ENTRYPOINT"} {
		if !strings.Contains(dockerfile, expected) {
			t.Fatalf("backend/Dockerfile does not mention %q", expected)
		}
	}
}

// TestEnvExampleDocumentsEveryConfigurationKey keeps .env.example honest: a new
// configuration key without documentation fails the build.
func TestEnvExampleDocumentsEveryConfigurationKey(t *testing.T) {
	envExample := readRepoFile(t, ".env.example")
	documented := make(map[string]bool)

	for _, line := range strings.Split(envExample, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _ := strings.Cut(line, "=")
		documented[key] = true
	}

	for _, key := range config.Keys() {
		envName := config.EnvName(key)
		if !documented[envName] {
			t.Fatalf("configuration key %q (%s) is not documented in .env.example", key, envName)
		}
	}
}

// TestComposePassesEveryBackendKeyToBothProcesses proves the compose file is not
// missing a key an operator would otherwise have to discover from the code.
func TestComposePassesEveryBackendKeyToBothProcesses(t *testing.T) {
	compose := loadCompose(t)

	for _, serviceName := range []string{"api", "worker"} {
		service := compose.Services[serviceName]
		for _, key := range config.Keys() {
			envName := config.EnvName(key)
			if _, ok := service.Environment[envName]; !ok {
				t.Fatalf("service %q does not pass %s to the process", serviceName, envName)
			}
		}
	}
}

// TestFreeRADIUSSchemaIsLoadedOnFirstBoot checks that the MySQL service brings
// the radcheck/radreply tables the provisioning slice will write to.
func TestFreeRADIUSSchemaIsLoadedOnFirstBoot(t *testing.T) {
	schema := readRepoFile(t, "docker/freeradius-mysql/init/01-schema.sql")

	for _, table := range []string{"radcheck", "radreply"} {
		if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("FreeRADIUS init script does not create %s", table)
		}
	}
}
