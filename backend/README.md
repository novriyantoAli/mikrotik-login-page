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
| `internal/application/transaction` | Transaction state machine, idempotent checkout, gateway callback, expiry sweep (ADR 0015) |
| `internal/application/payment` | The `Gateway` port and its Tripay adapter (`payment/tripay`), the only code that knows the vendor |
| `internal/application/{notification,provisioning}` | Vendor ports (SMS sender, FreeRADIUS writer and expiration reader) |
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

## Checkout and settlement (#14)

Every endpoint below needs the session cookie
(`middleware.SessionGuard.Require()`), except the gateway callback, which
authenticates its raw body with the shared secret instead.

| Endpoint | Body |
| --- | --- |
| `GET /api/transactions/channels` | `{"data":[{"code","name","group","fee_flat_idr","fee_percent"}]}` from the gateway, fees separate from the Package price |
| `POST /api/transactions` | `{"package_id","channel","return_url"?}` → `200 {"data":{…Transaction}}`; a second call for the same Package returns the same live pending Transaction |
| `GET /api/transactions/{id}` | `200 {"data":{…Transaction}}`; another buyer's Transaction is `404`, never `403` |
| `POST /api/payments/callback` | public; `X-Callback-Signature` over the raw body. The only path into `paid` (ADR 0008/0015) |

A Transaction is `pending`, `paid`, `provisioned`, `failed` or `expired`; the
allowed transitions are `pending → paid|failed|expired` and
`paid → provisioned`, enforced by guarded `UPDATE … WHERE state = …` statements.
A repeated callback, a late settlement and a repeated "Bayar" tap all lose that
guard and change nothing. Pending Transactions whose deadline passed are expired
by the worker sweep (`transaction.expiry_sweep_interval`), never by a client.

Configuration: `transaction.payment_ttl`, `transaction.expiry_sweep_interval`,
and `tripay.{api_key,private_key,merchant_code,base_url,callback_url,
return_url,request_timeout}`. Empty Tripay credentials still boot; checkout
answers `502 dependency_unavailable` until they are set.

Fixtures live in `../contracts/{channels,transaction*}.json` and are decoded by
both the Go integration tests and the app's strict zod schemas.

## Provisioning a paid Transaction (#15)

Settlement only queues the work. The worker owns it, because that process holds
the FreeRADIUS connection and the modem (ADR 0016):

1. A verified settlement moves the Transaction to `paid` and enqueues the
   payload-free task `transaction:provision`. The durable work item is the paid
   row itself, so a lost task costs one sweep, never the account.
2. The worker provisions every paid Transaction on `pin.sweep_interval`
   (default 30 s), which is also how a failed attempt is retried.
3. The first attempt computes the target Active Period **once** — an Active
   Period still running is extended, an absent or finished one starts at the
   settlement time (PRD 21) — and generates a fresh 6-digit PIN, storing both on
   the Transaction with a set-once `UPDATE`. A retry reuses them: it cannot add
   the duration twice and cannot rotate the PIN.
4. `radcheck`/`radreply` are upserted in one MySQL transaction:
   `Cleartext-Password := <PIN>`, `Expiration := <epoch seconds>`,
   `Mikrotik-Rate-Limit = <Package rate limit>`.
5. `paid → provisioned` and the PIN SMS row commit in one Postgres transaction,
   so "provisioned but never queued" cannot happen. A dead modem leaves the
   Transaction `provisioned` with the SMS pending in the outbox.

`GET /api/transactions/{id}` always carries `phone_number` and carries `pin`
once the Transaction is `provisioned`. The PIN never appears in a list response,
is never logged, and is stored only as AES-256-GCM ciphertext together with its
key version. Configuration: `pin.encryption_keys` (`version=base64(32 bytes)`,
comma separated), `pin.active_key_version`, `pin.sweep_interval`. Rotate by
adding a key and bumping the active version; keep the old key until no row names
its version.

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
  wires the production router and dependencies, decorates the production
  adapters with the fakes and exposes them for assertions;
  `WithRealRadiusReader()` keeps the MySQL adapter for tests of the degraded
  FreeRADIUS path. `harness.TransactionProvisioning` drives provisioning the way
  the worker does, because the harness runs the API process.
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
