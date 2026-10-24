package integration

import (
	"fmt"
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
	Image       string              `yaml:"image"`
	Command     []string            `yaml:"command"`
	Environment map[string]string   `yaml:"environment"`
	Ports       []string            `yaml:"ports"`
	DependsOn   composeDependencies `yaml:"depends_on"`
	Healthcheck composeHealthcheck  `yaml:"healthcheck"`
}

type composeHealthcheck struct {
	Test        []string `yaml:"test"`
	StartPeriod string   `yaml:"start_period"`
}

type composeDependency struct {
	Condition string `yaml:"condition"`
}

// composeDependencies accepts both compose spellings: the short list form and
// the long map form with a condition.
type composeDependencies map[string]composeDependency

// UnmarshalYAML implements yaml.Unmarshaler for both depends_on shapes.
func (d *composeDependencies) UnmarshalYAML(value *yaml.Node) error {
	parsed := make(map[string]composeDependency)

	switch value.Kind {
	case yaml.SequenceNode:
		var names []string
		if err := value.Decode(&names); err != nil {
			return err
		}
		for _, name := range names {
			parsed[name] = composeDependency{}
		}
	case yaml.MappingNode:
		if err := value.Decode(&parsed); err != nil {
			return err
		}
	default:
		return fmt.Errorf("depends_on must be a list or a map")
	}

	*d = parsed
	return nil
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
		"migrate",
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

// TestMigrateRunsBeforeTheAPI pins ADR 0012's deployment shape: the schema and
// the Package seed are applied by a one-shot command that the API waits for,
// instead of every API boot migrating the database.
func TestMigrateRunsBeforeTheAPI(t *testing.T) {
	compose := loadCompose(t)

	migrate, ok := compose.Services["migrate"]
	if !ok {
		t.Fatal("docker-compose.yml has no migrate service")
	}
	if strings.Join(migrate.Command, " ") != "migrate" {
		t.Fatalf("migrate command = %v", migrate.Command)
	}
	if migrate.Image != compose.Services["api"].Image {
		t.Fatal("migrate must run from the same image as the API")
	}

	for _, serviceName := range []string{"api", "worker"} {
		dependency, ok := compose.Services[serviceName].DependsOn["migrate"]
		if !ok {
			t.Fatalf("%s does not depend on migrate", serviceName)
		}
		if dependency.Condition != "service_completed_successfully" {
			t.Fatalf("%s waits for migrate with condition %q, want service_completed_successfully",
				serviceName, dependency.Condition)
		}
	}

	if !strings.Contains(readRepoFile(t, "backend/Dockerfile"), "./cmd/migrate") {
		t.Fatal("backend/Dockerfile does not build ./cmd/migrate")
	}
	if !strings.Contains(readRepoFile(t, "backend/docker/entrypoint.sh"), "migrate") {
		t.Fatal("backend/docker/entrypoint.sh does not dispatch the migrate command")
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

	for _, serviceName := range []string{"migrate", "api", "worker"} {
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

// TestWorkerIsHealthcheckedThroughItsOwnListener pins ADR 0018's answer to "what
// does a healthy worker mean": the container is polled on the worker's operator
// listener, not on Redis and not by trusting that the process is running.
func TestWorkerIsHealthcheckedThroughItsOwnListener(t *testing.T) {
	compose := loadCompose(t)

	worker, ok := compose.Services["worker"]
	if !ok {
		t.Fatal("docker-compose.yml has no worker service")
	}

	healthcheck := strings.Join(worker.Healthcheck.Test, " ")
	if !strings.Contains(healthcheck, "/healthz") {
		t.Fatalf("worker healthcheck %q does not poll /healthz", healthcheck)
	}
	if strings.Contains(healthcheck, "redis-cli") {
		t.Fatal("the worker healthcheck proves Redis, not the worker")
	}
	if worker.Healthcheck.StartPeriod == "" {
		t.Fatal("worker healthcheck has no start_period: the first sweep would fail it")
	}

	// Operator-only: one loopback binding, and the proxy never routes it.
	if len(worker.Ports) != 1 || !strings.HasPrefix(worker.Ports[0], "127.0.0.1:") {
		t.Fatalf("worker ports = %v, want exactly one loopback binding", worker.Ports)
	}
	if strings.Contains(readRepoFile(t, "docker/proxy/nginx.conf"), "8081") {
		t.Fatal("the proxy routes the worker's operator listener onto the public origin")
	}
}
