package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"gorm.io/gorm"

	appfx "github.com/novriyantoAli/mikrotik-login-page/backend/internal/app"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/notification/sms"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/packages/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/payment"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/provisioning"
	transactionservice "github.com/novriyantoAli/mikrotik-login-page/backend/internal/application/transaction/service"
	"github.com/novriyantoAli/mikrotik-login-page/backend/internal/config"
	serverapi "github.com/novriyantoAli/mikrotik-login-page/backend/internal/server/api"
	servermigration "github.com/novriyantoAli/mikrotik-login-page/backend/internal/server/migration"
)

// APIHarness runs the production HTTP graph against real Postgres and Redis,
// with every vendor edge faked. Tests drive it through the router, never through
// service internals.
type APIHarness struct {
	App          *fx.App
	Engine       *gin.Engine
	Config       *config.Config
	Doubles      *TestDoubles
	Dependencies *Dependencies

	// Schema is the throwaway Postgres schema this test is bound to.
	Schema string
	// Postgres is a harness-owned handle on that schema, for seeding and
	// asserting rows.
	Postgres *gorm.DB

	Gateway      payment.Gateway
	SMSSender    sms.Sender
	RadiusWriter provisioning.Writer

	// Catalog is the Package catalog service, and Migrator the deployment
	// migration step, so a test can re-run the seed against its own schema.
	Catalog  service.Service
	Migrator *servermigration.Migrator
	// TransactionExpiry drives the payment deadline in tests: the worker sweeps
	// in production, and the test advances the frozen clock and sweeps once.
	TransactionExpiry transactionservice.Expiry
}

// HarnessOption adjusts one harness before it starts.
type HarnessOption func(*harnessSettings)

type harnessSettings struct {
	redisAddr          *string
	freeRADIUSMySQLDSN *string
	doubles            []DoublesOption
}

// WithRedisAddr overrides the Redis instance used by the harness.
func WithRedisAddr(addr string) HarnessOption {
	return func(s *harnessSettings) { s.redisAddr = &addr }
}

// WithFreeRADIUSMySQLDSN overrides the FreeRADIUS MySQL instance; an empty
// string points the app at a closed port so the dependency reports "down".
func WithFreeRADIUSMySQLDSN(dsn string) HarnessOption {
	return func(s *harnessSettings) { s.freeRADIUSMySQLDSN = &dsn }
}

// WithRealRadiusReader keeps the production FreeRADIUS MySQL adapter instead of
// the in-memory fake, for tests of the degraded database path.
func WithRealRadiusReader() HarnessOption {
	return func(s *harnessSettings) { s.doubles = append(s.doubles, WithoutRadiusReaderFake()) }
}

// NewAPIHarness starts the API graph for one test and stops it afterwards.
func NewAPIHarness(t *testing.T, options ...HarnessOption) *APIHarness {
	t.Helper()

	deps := RequireDependencies(t)

	settings := harnessSettings{
		redisAddr:          &deps.RedisAddr,
		freeRADIUSMySQLDSN: &deps.FreeRADIUSMySQLDSN,
	}
	for _, option := range options {
		option(&settings)
	}

	schema := NewPostgresSchema(t, deps.PostgresDSN)
	ApplyPostgresEnv(t, deps.PostgresDSN, schema)
	ApplyRedisEnv(t, *settings.redisAddr)
	ApplyMySQLEnv(t, *settings.freeRADIUSMySQLDSN)

	// Production keeps the graph and Gin mode identical to the deployed process.
	t.Setenv("APP_ENVIRONMENT", config.EnvironmentProduction)
	t.Setenv("LOGGER_LEVEL", "warn")
	t.Setenv("LOGGER_FORMAT", config.LogFormatConsole)

	harness := &APIHarness{
		Dependencies: deps,
		Doubles:      NewTestDoubles(),
		Schema:       schema,
	}

	application := fx.New(
		fx.NopLogger,
		appfx.RouterWithBase(appfx.Core(), harness.Doubles.FxOption(settings.doubles...)),
		// The harness owns a throwaway schema, so it applies the same schema
		// and seed step deployment runs before the API (ADR 0012).
		servermigration.Module(),
		fx.Invoke(func(
			cfg *config.Config,
			router *serverapi.Router,
			gateway payment.Gateway,
			sender sms.Sender,
			writer provisioning.Writer,
			catalog service.Service,
			migrator *servermigration.Migrator,
			expiry transactionservice.Expiry,
		) {
			harness.Config = cfg
			harness.Engine = router.Engine
			harness.Gateway = gateway
			harness.SMSSender = sender
			harness.RadiusWriter = writer
			harness.Catalog = catalog
			harness.Migrator = migrator
			harness.TransactionExpiry = expiry
		}),
	)

	startCtx, cancelStart := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStart()
	if err := application.Start(startCtx); err != nil {
		t.Fatalf("start api harness: %v", err)
	}

	t.Cleanup(func() {
		stopCtx, cancelStop := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelStop()
		if err := application.Stop(stopCtx); err != nil {
			t.Logf("stop api harness: %v", err)
		}
	})

	harness.App = application
	harness.Postgres = OpenPostgres(t, deps.PostgresDSN, schema)
	t.Cleanup(func() {
		if sqlDB, err := harness.Postgres.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	return harness
}

// Migrate re-runs the deployment migration (schema plus Package seed) against
// this test's schema, so a test can prove it is idempotent.
func (h *APIHarness) Migrate(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := h.Migrator.Up(ctx); err != nil {
		t.Fatalf("re-run migration: %v", err)
	}
}

// Do sends one request through the production router.
func (h *APIHarness) Do(t *testing.T, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return h.DoRequest(t, method, path, body, nil)
}

// DoRequest sends one request and lets the caller adjust it before it reaches
// the router: cookies, headers and a distinct RemoteAddr (which rate limits key
// on) are all set this way.
func (h *APIHarness) DoRequest(
	t *testing.T,
	method, path string,
	body []byte,
	mutate func(*http.Request),
) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if mutate != nil {
		mutate(request)
	}

	recorder := httptest.NewRecorder()
	h.Engine.ServeHTTP(recorder, request)
	return recorder
}

// GetJSON performs a GET and decodes the response body into a generic map.
func (h *APIHarness) GetJSON(t *testing.T, path string) (int, map[string]any) {
	t.Helper()

	recorder := h.Do(t, http.MethodGet, path, nil)
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %s response %q: %v", path, recorder.Body.String(), err)
	}
	return recorder.Code, decoded
}
