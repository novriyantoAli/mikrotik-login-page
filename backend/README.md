# Backend (Go + Fx + Gin + GORM + Asynq)

The API serves the purchase app and the Login Page contracts; the worker runs
background jobs. Both come from one image with different commands.

Documentation: [`../docs/adr/`](../docs/adr), [`../CONTEXT.md`](../CONTEXT.md),
[`../docs/prd/transaction-purchase-app-backend.md`](../docs/prd/transaction-purchase-app-backend.md).

## Layout

| Path | Role |
| --- | --- |
| `cmd/api`, `cmd/worker` | Composition roots; one image, two commands |
| `internal/app` | Fx option sets shared by both processes and by tests |
| `internal/config` | Viper configuration (`config.yaml` + `SECTION_KEY` env overrides) |
| `internal/application/<domain>` | Vertical slice: `dto/`, `entity/`, `repository/`, `service/`, `handler/`, `worker/`, `module.go` |
| `internal/application/{payment,notification,provisioning}` | Vendor ports (gateway, SMS sender, FreeRADIUS writer) |
| `internal/pkg` | Infrastructure: `database`, `logger`, `clock`, `httpx`, `testutil` |
| `internal/server/{api,worker}` | Transports and Fx wiring |
| `test/integration` | HTTP-contract integration tests, process tests, deployment tests |

Layering is one direction only: `handler → service → repository → entity`.
Interfaces live next to their implementation, cross-domain calls go through the
other domain's service interface, and only DTOs cross the HTTP boundary.

## Configuration

Every knob is in `internal/config/config.go`; `config.sample.yaml` documents the
defaults and `../.env.example` documents the environment names. Deployed values
arrive as `SECTION_KEY` environment variables (compose does this); nothing is
hardcoded in domain code and no secret is committed.

`CONFIG_FILE` (not a `config.Keys()` entry) points at a YAML file instead of the
default `config.yaml` search path. The compose deployment passes environment
variables only, so it never sets it.

The API starts even when a dependency is unreachable: `GET /api/health` then
reports which one is down (503, `{"data": {"status": "degraded", ...}}`) instead
of the process crash-looping out of reach of the operator.

## Test harness (`internal/pkg/testutil`)

Integration tests assert HTTP behaviour, never internal calls.

- **Real Postgres** with a throwaway schema per test (`NewPostgresSchema`).
  `TestMain` starts an embedded Postgres when `TEST_POSTGRES_DSN` is unset, so a
  clean machine needs no database container; CI passes the DSN of a service
  container instead.
- **Real Redis** started from `redis-server` when `TEST_REDIS_ADDR` is unset.
- **FreeRADIUS MySQL** is external only (`TEST_FREERADIUS_MYSQL_DSN`). Without
  it the health test asserts the `down` path, so the suite is still meaningful.
- **Vendor fakes** behind the real ports: `FakeGateway`, `FakeSMSSender`,
  `FakeRadiusWriter`, and a `FakeClock`. `NewAPIHarness` wires the production
  router and dependencies and exposes the fakes for assertions.

`TestMain` in `test/integration` starts the dependencies:
`testutil.RunWithDependencies(m)`.

## Commands

```bash
make deps            # go mod download + tidy
make build           # bin/api + bin/worker
make run-api         # API on SERVER_HOST:SERVER_PORT
make run-worker      # Asynq worker
make test            # unit + integration (real Postgres, real Redis)
make test-unit       # internal/... only
make test-integration# test/integration only
make lint            # gofmt + go vet + golangci-lint (.golangci.yml)
make format          # gofmt + goimports (-local module path)
make format-strict   # format + gofumpt
make swagger-gen     # swaggo annotations -> ../docs/api
make tools           # install goimports, gofumpt, golangci-lint v2, swag
make typecheck       # go build + go vet
```
