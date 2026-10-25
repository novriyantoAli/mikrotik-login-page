# Reconciliation sweeps payable Transactions through `settle`, the worker owns the operator listener, and the smoke journey runs without a modem

Issue #17 is the hardening slice: the lifecycle already exists end to end, so
what is left is the work that makes it survive a lost callback, an operator at
3am, and CI. Four questions had to be settled before any of that code could be
written, plus one about how the experience criteria become executable.

## 1. Reconciliation is a sweep over *payable* `pending` rows that enters through `settle`

Decision: a third sweeper, `transaction:reconcile`, built on the same
`newSweeper` helper the expiry and provisioning sweeps use, so start/stop and
wake-up handling are not reimplemented. Its knobs are
`transaction.reconcile_sweep_interval` (default `1m`) and
`transaction.reconcile_batch` (default `50`), delivered through a local
`service.ReconciliationSettings` like every other domain setting.

The selector is the whole design:

```sql
SELECT id FROM transactions
WHERE state = 'pending' AND expires_at > $1
ORDER BY expires_at ASC
LIMIT $2;
```

`expired` is a final state (ADR 0015), and a settlement that arrives after the
deadline is already only a `Warn` in `settle`. Reconciling a row whose deadline
has passed would therefore produce a warning line every sweep for every late
payer and recover nothing — so "still payable" is the window, not a tuned
comparison of two intervals. Validation only insists that
`reconcile_sweep_interval <= payment_ttl`, because an interval longer than the
deadline would let a whole TTL pass without a single sweep, which is the one
thing this job exists to prevent.

One `Gateway.GetStatus` per row per sweep, mapped into `payment.Callback` and
handed to the same `settle` / `close` the callback uses:

| Vendor status | Path | Effect |
| --- | --- | --- |
| `paid` | `settle` | guarded `pending → paid`, then the provisioning wake-up |
| `failed`, `expired` | `close` | guarded `pending → failed/expired` |
| `unpaid` | — | nothing, the buyer simply has not paid yet |
| error from the vendor | — | `Warn` with the reference, state unchanged |

`settle` already refuses an amount mismatch and already treats a repeated
settlement as a lost race, so a reconciliation that overlaps a live callback is
safe by construction. A mismatch is logged as an error with both amounts and the
row stays `pending` until the deadline — the same outcome the callback path
gives with `409`.

Consequence: the worker now builds `tripay.Module()`. That is the price of
asking the gateway anything, and it stays honest because the adapter is a port
implementation, not a transport — the worker never serves the callback route.
`WorkerModule` gains the new registrar alongside `Expiry` and `Provisioning`.

Risk accepted: a payment made in the last sweep interval before the deadline can
still be missed (callback lost, sweep already ran, settlement lands after
`expires_at`). The operator's hook is the existing
`settlement arrived for a Transaction that can no longer be paid` warning with
its reference. Refunding or manually provisioning that money is out of scope
here.

Rejected alternatives: reconciling `expired` rows and transitioning backwards
(breaks the state machine ADR 0015 froze); marking `paid` straight from
`GetStatus` (a second door into the only state that provisions accounts); one
Asynq task per pending Transaction (a transaction has no durable `paid`-like
work item to re-read after a lost task, and the sweep is the same code for every
row); reconciling `paid` rows (the provisioning sweep already owns them).

## 2. Metrics live on the worker's operator listener, not on the public origin

Decision: `GET /metrics`, served next to `/healthz` on the worker's operator
listener (§3), returning a plain JSON body:

```json
{
  "outbox": { "pending": 0, "sending": 0, "sent": 128, "failed": 1,
              "oldest_pending_age_seconds": 0 },
  "provisioning": { "paid_awaiting": 0, "attempts": 0 }
}
```

Definitions, because a metric without one is a guess: `sending` counts rows
claimed but unfinished, which is how a stuck lease shows up;
`oldest_pending_age_seconds` is measured from the `created_at` of the oldest row
still owed (`pending` or `sending`) — how long a buyer has been waiting — and is
`0` when nothing is queued. A terminal `sent` or `failed` row is not a wait, so
the gauge can return to zero instead of growing forever on a dead message. `provisioning.paid_awaiting` is the count of `paid` rows, and
`provisioning.attempts` is `SUM(provision_attempts)` over those same rows: a
**gauge** that rises while a backlog is retrying and returns to zero when it
clears, not a monotonic counter. `transactions` gains the `provision_attempts`
column this needs, incremented best-effort at the start of each `Provision` —
observability must never be the reason provisioning fails, so a failed increment
is logged and the attempt continues.

This is a deviation from the `GET /api/metrics` sketch in the #17 handoff, and
the reason is worth recording: PRD point 31 froze the public surface (the two
Login Page reads, the gateway callback, the health check), and outbox depth is
operator data, not client data. Serving it from the worker keeps business volume
off the internet-facing origin and keeps `/api/health` and its two contract
fixtures untouched.

No envelope and no `contracts/` fixture: nothing shipped parses this body, it is
not part of the client contract, and a fixture would imply a stability promise
the operator surface does not have. `backend/README.md` documents the shape
instead, including the query definition of each number. Two aggregate queries
per call, no caching, no new dependency; when a Prometheus scrape target
eventually exists, the same numbers move behind the same path in the text
format.

Rejected alternatives: adding the numbers to `/api/health` (mixes dependency
health with business volume and breaks a fixture frozen on both sides);
`prometheus/client_golang` now (a dependency with no scrape target, plus a
registry to thread through every layer); a public `/api/metrics` (publishes
business volume on the same origin as the app; a deployment that wants it can
proxy it, but that should be a decision, not a default).

## 3. Worker health is the same report the API serves, plus sweep freshness

Decision: `WORKER_HEALTH_ADDR` (config `worker.health_addr`, default `:8081`,
empty disables the listener) starts a small stdlib `http.Server` inside the
worker process serving `GET /healthz` and the `/metrics` above. The worker
imports no Gin and mounts no router: it is one handler, one listener, two
lifecycle hooks.

The body is `health/dto.Health` — the same report `/api/health` returns — because
"healthy" has to mean the same thing in both processes. To get there without
duplicating probes, the three dependency checkers that only `database.Module()`
contributed move into a `database.CheckersModule()`, the worker graph adds
`health.Module()` plus that option, and the worker contributes one more checker,
`sweeps`. That checker fails when any sweeper's last completed run is older than
three times its own interval, which is the one failure the dependency probes
cannot see: Redis answers, Postgres answers, and the process has silently
stopped sweeping. Each sweeper therefore records a heartbeat (name, interval,
last completed run) in one registry shared with the checker — a small addition
to `newSweeper`, applied to all three callers in the same change.

Compose: the worker gets a `healthcheck` of
`wget -q -O /dev/null http://127.0.0.1:8081/healthz` with `start_period: 60s`
so the first sweep has a chance to run, and the port is published as
`127.0.0.1:8081:8081` only — reachable by an operator on the host, never through
the proxy. Alpine's busybox already provides `wget`, so the runner image gains
no package for this. `/healthz` answers `200` when every check is up and `503`
when any is not, exactly like `/api/health`, so a failing check names the broken
dependency instead of asking an operator to guess.

Rejected alternatives: `redis-cli ping` as the worker healthcheck (that proves
Redis, not the worker — the worker could be wedged with Redis perfectly fine);
treating a `running` container as a healthy one (`restart: unless-stopped`
already means a crashed worker is restarted; the failure that hurts is the live
one); serving this on the API port (the worker would need Gin and the public
surface for one internal probe); a second, worker-shaped health DTO (two
definitions of "degraded" to keep in sync).

## 4. Structured events with stable names, emitted where the transition is decided

Decision: every Transaction state change emits one line with
`event=transaction.state_changed` and the fields `transaction_id`, `from`, `to`,
`reference`, `phone` (already through `phone.Mask`) and `reason`, where `reason`
is one of `gateway_callback`, `reconciliation`, `expiry_sweep`,
`checkout_superseded`, `provisioning`. Every provisioning attempt emits
`provisioning.attempt` with `transaction_id`, `attempt` (from
`provision_attempts`), `outcome` ∈ {`ok`, `retry`} and `error` when the outcome is
`retry`. There is no `failed` outcome: a paid Transaction is never abandoned
(ADR 0016), so every unsuccessful attempt is one the next sweep retries — a third
value would name a state that cannot occur.

The two bulk expiry paths stay one guarded `UPDATE` each (ADR 0015) and gain a
`RETURNING id, reference, phone_number`, so the per-row claim above holds without
a second read and without turning the sweep into a per-Transaction loop.

The reason is supplied by the caller of `settle` / `close` rather than sniffed
inside them, which is why threading it through reconciliation belongs to the
reconciliation change and not to a later tidy-up: one code path writes one
shape, and a log query can group by reason without parsing prose. The existing
human lines ("transaction paid", "transaction provisioned") stay where they
carry context; the rule being added is stable keys, not more prose.

Never logged, under any of these events: the PIN, an unmasked Phone Number, or
the callback body (attacker-controlled, a convention `callback.go` already
keeps).

Rejected alternatives: logging from the repository (data access would own the
business vocabulary, and it sees every guarded `UPDATE` including the ones that
changed nothing); per-caller ad-hoc messages (an operator cannot query for an
event they cannot name); a metrics-only signal with no log line (the log is what
explains *why* a state changed).

## 5. The smoke journey proves the chain with the modem absent and the vendor stubbed

Decision: handoff option (c). A `docker-compose.e2e.yml` override makes two
vendor edges test-controlled without a line of fake-mode code in production:

- `SMS_DEVICE` names a device that does not exist, so the drainer fails the send
  and the row keeps its `message`; only a `sent` row is emptied (ADR 0010). That
  is also PRD point 24 proven rather than assumed: a dead modem does not
  cost a paid buyer the **PIN**.
- `TRIPAY_BASE_URL` points at a stub that speaks only the two endpoints this
  journey needs and posts its own settlement callback, signed with the same test
  private key the backend verifies.

The Playwright test then walks the real stack: Login Page (the existing static
server, given an optional API-proxy origin so the same page talks to the compose
API; its default behaviour is unchanged and the `tests/smoke` suite keeps
running), pick a Package, submit a Phone Number, read the Magic Link out of the
undelivered `sms_outbox.message` through `docker compose exec postgres psql`,
open it, pick a channel, pay on the stub's page, wait for the **PIN** in
**Account Status**, then return to the Login Page.

The stub posts the callback instead of the test calling it directly on purpose:
the settlement must arrive as a signed vendor notification, so signature
verification stays on the real path instead of being bypassed by the test.

Boundary, stated so nobody over-reads the result: this proves the app, API,
worker, Postgres, MySQL FreeRADIUS, the outbox and the callback path agree on
one journey. It does not prove RouterOS, the modem, or Tripay. Handoff option
(a) — a pty AT emulator — is the honest fix for the modem edge and is budgeted
as separate work, not smuggled in here; option (b), seeding a session, would
skip exactly the step most likely to break.

CI: a separate `E2E (compose)` job — bring the stack up with both compose files,
wait for `/api/health`, run the spec, and `down -v` in `if: always()`. Separate
so a journey failure is attributable, and so the backend job stays the fast
signal it is today.

**Status: this section is a decision, not yet shipped.** The compose override, the
vendor stub, the journey spec and the `E2E (compose)` job land in the remaining
part of #17, which needs a host with Docker; until that job is green this
criterion is unmet. Everything else in this ADR is in the tree.

## 6. The experience pass is executable, not a review note

Decision: two checks instead of a manual sweep.

- `frontend/tests/unit/ui-language.test.ts` reads every
  `frontend/src/**/*.svelte`, strips the `<script>` and `<style>` blocks, and
  matches the forbidden terms (`voucher`, `order`, `renew`, `top-up`/`topup`,
  `dashboard`, `backoffice`) case-insensitively against the rendered text and the
  user-facing attributes (`label`, `title`, `placeholder`, `aria-label`, `alt`).
  Matching rendered text is what keeps CSS such as `order:` and ordinary
  identifiers out of the failure list, so the test needs no growing allowlist.
- 360px: `document.documentElement.scrollWidth <= clientWidth` is asserted at a
  360×740 viewport on every screen of the app. jsdom cannot measure layout, so
  this belongs in a real browser: today that is `tests/app/layout.spec.mjs`,
  which builds the SPA, serves it with `vite preview` and answers `/api/**` from
  the contract fixtures, so it needs no backend. When §5 lands, the same
  assertion folds into the journey spec and the stand-alone project goes away.

Rejected alternatives: a one-off manual sweep (an acceptance criterion only a
human can check rots by the next commit); scanning the raw source (false
positives on CSS and identifiers force a skip list that grows until the test
means nothing); waiting for the E2E to exist before any 360px check runs at all
(the layout criterion is decidable today, and a browser boot is cheaper than an
unverified claim).

## Consequences

- Three new configuration keys, each in four places or
  `test/integration/deployment_test.go` fails: `transaction.reconcile_sweep_interval`,
  `transaction.reconcile_batch`, `worker.health_addr`.
- One new column, `transactions.provision_attempts`, picked up by AutoMigrate
  from the entity. New reads stay repository-owned: `ReconcilableIDs` and the
  state counters for the outbox and provisioning metrics; no `dto.*` reaches
  persistence.
- The worker gains the Tripay adapter, the health service and an operator HTTP
  listener; the API's route table, `/api/health` body and both health fixtures
  do not change.
- Load-bearing files: `transaction/worker/reconciliation.go`,
  `transaction/service/reconcile.go`, `notification/repository` stats,
  `database.CheckersModule()`, the worker health listener, `tests/e2e/`, and
  `frontend/tests/unit/ui-language.test.ts`.
- After this slice, PRD #10 is complete. The **Admin Panel** is the only
  remaining area and it has no PRD yet, so it starts with one rather than with
  code.
