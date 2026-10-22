# Backend (Go + Fx + Gin + GORM + Asynq)

The API serves the purchase app and the Login Page contracts; the worker runs
background jobs; `migrate` applies the schema and the Package seed. All three
come from one image with different commands.

Documentation: [`../docs/adr/`](../docs/adr), [`../CONTEXT.md`](../CONTEXT.md),
[`../docs/prd/transaction-purchase-app-backend.md`](../docs/prd/transaction-purchase-app-backend.md).

## Layout

| Path | Role |
| --- | --- |
| `cmd/api`, `cmd/worker`, `cmd/migrate` | Composition roots; one image, three commands |
| `internal/app` | Fx option sets shared by both processes and by tests |
| `internal/config` | Viper configuration (`config.yaml` + `SECTION_KEY` env overrides) |
| `internal/application/<domain>` | Vertical slice: `dto/`, `entity/`, `repository/`, `service/`, `handler/`, `worker/`, `module.go` |
| `internal/application/packages` | Package catalog: seed data, public `GET /packages` |
| `internal/application/activeperiod` | Public `GET /active-period`, read through the provisioning port |
| `internal/application/{payment,notification,provisioning}` | Vendor ports (gateway, SMS sender, FreeRADIUS writer and expiration reader) |
| `internal/pkg` | Infrastructure: `database`, `logger`, `clock`, `httpx`, `testutil` |
| `internal/server/{api,worker,migration}` | Transports, the migration step, Fx wiring |
| `test/integration` | HTTP-contract integration tests, process tests, deployment tests |
| `../contracts` | Response fixtures shared by the Go tests and the Login Page tests |

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

## Deployment migration (ADR 0012)

`migrate` applies the Postgres schema (`packages`, later the other tables from
the PRD) and seeds the starter Package catalog, then exits. Deployment runs it
as a one-shot compose service and the API waits for it
(`service_completed_successfully`); the API and worker never migrate
themselves.

The seed keys on `packages.code` with `ON CONFLICT DO NOTHING`, so running the
command twice inserts nothing, and a price an operator changed by hand survives
a redeploy. Until the Admin Panel exists, SQL is the only way to edit the
catalog, and changing a starter value in code does not rewrite existing rows.

## Public read contract (Login Page)

| Endpoint | Body |
| --- | --- |
| `GET /api/packages` | `{"data":[{"id","nama","harga","durasiActivePeriod","batasKecepatan"}]}` |
| `GET /api/active-period?phone=…` | `{"data":{"expiry":"<ISO-8601>"}}`, or `{"data":{"expiry":null}}` when the Phone Number has no Active Period |

Both are public and read-only, and answer `Access-Control-Allow-Origin: *` for
safe methods because RouterOS serves the Login Page from each hotspot's own
origin (ADR 0014). The Phone Number is normalised (`08…`, `+62…`,
spaced → `628…`); a malformed one is `400 invalid_request`, while an unreachable
FreeRADIUS MySQL is `503 dependency_unavailable`. The shapes are frozen by the
Login Page and pinned by `contracts/*.json` on both sides.

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
  `FakeRadiusWriter`, `FakeRadiusReader`, and a `FakeClock`. `NewAPIHarness`
  wires the production router and dependencies and exposes the fakes for
  assertions; `WithRealRadiusReader()` keeps the MySQL adapter for tests of the
  degraded FreeRADIUS path.
- **Migrations** run against the throwaway schema when the harness starts, the
  same step deployment runs, so tests see the seeded catalog.

`TestMain` in `test/integration` starts the dependencies:
`testutil.RunWithDependencies(m)`.

## Commands

```bash
make deps            # go mod download + tidy
make build           # bin/api + bin/worker + bin/migrate
make run-api         # API on SERVER_HOST:SERVER_PORT
make run-worker      # Asynq worker
make run-migrate     # schema + Package seed, then exit (ADR 0012)
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
