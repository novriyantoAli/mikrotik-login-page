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
| `internal/application/transaction` | Transaction state machine, idempotent checkout, gateway callback, expiry sweep, Transaction history (ADR 0015) |
| `internal/application/account` | Account Status read composition: summary, current PIN, active Package and Extend preview (ADR 0017); no entity, no table |
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
hardcoded in domain code and no secret is committed. A new key therefore lands
in four places — `config.go`, `config.sample.yaml`, `../.env.example` and
`../docker-compose.yml` — and `test/integration/deployment_test.go` fails when
one is missed.

`CONFIG_FILE` (not a `config.Keys()` entry) points at a YAML file instead of the
default search path, which is `config.yaml` (or `config.yml`) in the working
directory. Both names are git-ignored, so a local copy of `config.sample.yaml`
named either way never carries a secret into a commit. The compose deployment
passes environment variables only, so it never sets it.

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
| `GET /api/transactions?page=&page_size=` | `200 {"data":[…],"total_count","page","page_size"}`, newest first, **without** `pin` |
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
once the Transaction is `provisioned`. The PIN never appears in a list response
and is never logged. On the Transaction it is stored only as AES-256-GCM
ciphertext together with its key version; the outbox row is the one deliberate
clear-text copy, because the modem needs the digits, and its body is blanked as
soon as the row reaches `sent` or `failed`. Configuration: `pin.encryption_keys`
(`version=base64(32 bytes)`, comma separated), `pin.active_key_version`,
`pin.sweep_interval`. Rotate by adding a key and bumping the active version;
keep the old key until no row names its version.

## Account Status and Extend (#16, ADR 0017)

`account` is a read composition, not a stored record: PRD point 6 keeps **Phone
Number** as the only identity and FreeRADIUS as the truth for credentials and
expiry, so there is no `accounts` table. Every endpoint needs the session cookie.

| Endpoint | Body |
| --- | --- |
| `GET /api/account` | `{"data":{"phone_number","active_period":{"expires_at","found","state"},"package":{"id","name","rate_limit"}\|null,"pin"}}` |
| `GET /api/transactions?page=&page_size=` | `{"data":[…History],"total_count","page","page_size"}`, no `pin` |
| `GET /api/account/extend-preview?package_id=` | `{"data":{"package_id","current_expiration","estimated_expiration","extends"}}` |

`active_period.state` is one of `active`, `expiring`, `expired`, `none` and is
computed **here**, from the injected clock and `account.expiry_warning_hours`
(default 24, which must equal `expiryWarningHours` in
`../KOPINETGO-2024/config/data/voucher.js`), so the app only renders it and the
two pages cannot disagree about "almost over". `none` separates "never bought"
from "bought and it ran out".

`GET /account/extend-preview` calls the same `provisioning.Target` rule the
provisioner uses (an Active Period still running is extended, otherwise the new
one starts now), so the number the buyer confirms is the number provisioning
applies. The PIN is returned only by the summary and by the single-Transaction
read of a `provisioned` Transaction; no list ever carries one. A Phone Number
named in a query is only a cross-check against the session: a foreign one answers
`404`, never `403`.

Fixtures: `../contracts/{account,account.extend-preview,transactions}.response.json`,
decoded by both the Go integration tests and the app's strict zod schemas.

## Reconciliation, observability and the worker's operator listener (#17, ADR 0018)

A settlement whose callback never arrives is recovered by a third sweep:
`transaction:reconcile` runs on `transaction.reconcile_sweep_interval` (default
1 min) and asks `Gateway.GetStatus` about at most `transaction.reconcile_batch`
(default 50) transactions per pass. The selector is the window — `pending` rows
with `expires_at > now`, soonest deadline first — because `expired` is final, so
a sweep slower than the deadline would only produce warnings. The answer enters the
same `settle`/`close` path a callback uses, so provisioning cannot happen twice.

Every state change logs one structured line, `event=transaction.state_changed`,
with `transaction_id`, `from`, `to`, `reference`, `phone` (masked) and `reason`
(`gateway_callback`, `reconciliation`, `expiry_sweep`, `checkout_superseded`,
`provisioning`). Every provisioning attempt logs `event=provisioning.attempt`
with `attempt` (the row's `provision_attempts`) and `outcome` (`ok` or `retry` —
a paid Transaction is never abandoned, so there is no `failed`). Neither the PIN
nor an unmasked Phone Number ever reaches a log line.

The worker serves an internal surface on `worker.health_addr` (default `:8081`,
empty disables it). Compose publishes it on `127.0.0.1` only and healthchecks it;
the proxy never routes it.

| Path | Body |
| --- | --- |
| `GET /healthz` | the same `health/dto.Health` report as `/api/health` (200 when every check is up, 503 otherwise) |
| `GET /metrics` | `{"outbox":{"pending","sending","sent","failed","oldest_pending_age_seconds"},"provisioning":{"paid_awaiting","attempts"}}` |

`/healthz` carries the three dependency probes **plus** `sweeps`: each periodic
job records a heartbeat when a pass completes, and the check fails when any of
them is more than three intervals late — the one failure a dependency probe
cannot see. `oldest_pending_age_seconds` is measured from the `created_at` of the oldest row
still owed (`pending` or `sending`) — how long a buyer has been waiting; a
terminal `sent` or `failed` row is not a wait — and `provisioning.attempts`
sums the attempts on rows **still paid**, so both are gauges that return to zero
when the backlog clears. The numbers are read on demand with two aggregate
queries; there is no metrics dependency and no scrape configuration yet.

## Test harness (`internal/pkg/testutil`)

Integration tests assert HTTP behaviour, never internal calls.

- **Real Postgres** with a throwaway schema per test (`NewPostgresSchema`).
  `TestMain` starts an embedded Postgres when `TEST_POSTGRES_DSN` is unset, so a
  clean machine needs no database container; CI passes the DSN of a service
  container instead.
- **Real Redis** started from `redis-server` when `TEST_REDIS_ADDR` is unset.
  The Magic Link rate-limit counters live in that shared Redis with no test
  namespace, so a test that hits a rate limit must use its own Phone Number
  **and** its own `RemoteAddr`: reusing either leaks a spent counter into the
  next test.
- **FreeRADIUS MySQL** is external only (`TEST_FREERADIUS_MYSQL_DSN`). Without
  it the health test asserts the `down` path, so the suite is still meaningful.
  In CI the DSN is mandatory: `TestMain` exits non-zero when `CI` is set and the
  DSN is empty, because a skipped write test would otherwise leave the job green
  (ADR 0016).
- **Vendor fakes** behind the real ports: `FakeGateway`, `FakeSMSSender`,
  `FakeRadiusWriter`, `FakeRadiusReader`, and a `FakeClock`. `NewAPIHarness`
  wires the production router and dependencies, decorates the production
  adapters with the fakes and exposes them for assertions;
  `WithRealRadiusReader()` keeps the MySQL adapter for tests of the degraded
  FreeRADIUS path. `harness.TransactionProvisioning` drives provisioning the way
  the worker does, because the harness runs the API process. A test that needs
  time to pass moves `FakeClock`; `time.Sleep` is only acceptable in the process
  tests' bounded polls, never to wait for a state change.
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
