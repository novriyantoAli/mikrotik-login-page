# MikroTik Login Page — Voucher Purchase

System for managing internet hotspot voucher purchases: the MikroTik login page
displays a package list from the backend, the user is directed to a web app
(Svelte) to buy an active period, then logs in to the hotspot using their phone
number and PIN.

Domain vocabulary is binding — see [`CONTEXT.md`](CONTEXT.md). Architecture
decisions live in [`docs/adr/`](docs/adr/), the full requirements in
[`docs/prd/login-page-voucher-purchase.md`](docs/prd/login-page-voucher-purchase.md)
(Login Page) and
[`docs/prd/transaction-purchase-app-backend.md`](docs/prd/transaction-purchase-app-backend.md)
(backend + purchase app).

## Status

Every in-scope requirement of both PRDs is delivered: the Login Page integration
(public Package list, credential modes, Active Period) and the backend +
purchase app (Magic Link, payment, provisioning, Account Status and Extend, then
the hardening slice of ADR 0018). No slice of either PRD remains unstarted.

The **Admin Panel** — managing the Package catalog, listing Transactions, and
listing active hotspot users — is the only remaining area, and it has no PRD
yet, so it starts with requirements rather than with code (ADR 0018). Until it
exists the catalog is the deployment seed plus whatever an operator changed by
hand in SQL, and a Package deleted by hand comes back on the next deploy
(ADR 0012, `backend/README.md` § Deployment migration).

One known gap is budgeted outside that plan: the smoke journey runs with the
modem absent, so a pty AT emulator for the modem edge is separate work, not a
half-finished slice (ADR 0018 §5).

## Monorepo layout

| Path | Role |
| --- | --- |
| `KOPINETGO-2024/` | The MikroTik hotspot Login Page and status page (served by RouterOS) |
| `backend/` | Go API + worker (Fx, Gin, GORM, Asynq) — see [`backend/README.md`](backend/README.md) |
| `frontend/` | SvelteKit purchase app, built as a SPA — see [`frontend/README.md`](frontend/README.md) |
| `contracts/` | Shared request/response fixtures imported by backend and frontend tests |
| `docker-compose.yml` | One origin: Postgres, MySQL FreeRADIUS, Redis, API, worker, app, proxy |
| `tests/unit`, `tests/smoke` | Vitest + jsdom and Playwright tests of the Login Page seam |
| `docs/` | ADRs and PRDs |

The backend and purchase app are built fresh in this repo (ADR 0007); the
sibling `~/Projects/mikrotik-login` monorepo is prior art only.

## Quick start

```bash
cp .env.example .env      # fill in the secrets
make up                   # docker compose up -d --build (runs the migrate step first)
open http://localhost:8080/            # purchase app (status screen)
curl http://localhost:8080/api/health   # backend + Postgres + MySQL + Redis
make down
```

The one-shot `migrate` service applies the Postgres schema and the Package
catalog seed before the API starts (ADR 0012); `make -C backend run-migrate`
does the same against a local database.

The proxy serves the app at `/` and the API at `/api` on one origin (ADR 0009,
ADR 0013). `GET /api/health` answers 200 when every dependency is reachable and
503 with a `degraded` report when one is not, so an operator sees which server is
down rather than a blank failure.

### TLS

The compose proxy listens on plain HTTP by design: TLS terminates in front of it
(a host reverse proxy, a load balancer, or a certificate sidecar). The proxy
forwards the original scheme in `X-Forwarded-Proto`, so the backend sees `https`
and can mark the session cookie `Secure` (ADR 0009) and build `https://` Magic
Link URLs. Do not expose the proxy's HTTP port directly to the internet.

Only the reverse proxy may set `X-Forwarded-For`: `SERVER_TRUSTED_PROXIES` lists
the CIDRs the backend believes, and is empty (trust nobody) unless configured.

## Development and tests

Linters: `make lint` needs golangci-lint v2 and the frontend needs the ESLint
dev dependencies; `make -C backend tools` installs goimports, gofumpt,
golangci-lint and swag in one go.

Prerequisites: Go 1.26+, Node 22+, and `redis-server` in `PATH` (or a
`TEST_REDIS_ADDR` pointing at a running instance). The first backend test run
downloads a Postgres distribution for the embedded test server and caches it;
offline machines need `TEST_POSTGRES_DSN` to point at an existing Postgres.

```bash
make install              # root npm, backend go modules, frontend npm
make test                 # Login Page + backend integration + purchase app
make test-login           # Vitest + jsdom for the Login Page seam
make test-backend         # backend unit + integration (real Postgres, real Redis)
make test-frontend        # purchase app unit/component/contract tests
make typecheck            # tsc (login page), go build/vet, svelte-check
make lint                 # golangci-lint v2 + go vet, ESLint + Prettier
npm run test:smoke        # Playwright browser tests for the Login Page
npm run test:app          # Playwright 360px layout checks for the purchase app
make test-e2e             # full journey against the compose stack (needs Docker)
```

`make test-e2e` is the one suite that does not fake the stack: it brings the
compose services up, points the vendor at `docker/tripay-stub` and leaves the
modem absent, then walks Login Page → Package → Magic Link → payment → **PIN** in
Account Status → back to the Login Page (ADR 0018 §5). The Magic Link is read
from the undelivered `sms_outbox.message`, because the stored session is a hash
and the token row never keeps the token. Without Docker, the same spec runs
against a stack you already have: set `E2E_APP_URL`, `E2E_STUB_URL`,
`E2E_LOGIN_PAGE_URL` and — when `sms_outbox` is not reachable through
`docker compose exec` — `E2E_PSQL_CMD`, then

```bash
npx playwright test -c playwright.e2e.config.mjs --project=stub
```

`E2E_SANDBOX=1 … --project=sandbox` instead checks the real Tripay sandbox: it
creates a payment and stops, so it never settles money and needs no public
callback ingress. Run it when the vendor announces a change, not on every PR.

`make test-backend` needs a real Postgres and a real Redis. When
`TEST_POSTGRES_DSN` / `TEST_REDIS_ADDR` are unset the harness starts an embedded
Postgres and a local `redis-server` itself, so a clean machine needs no
containers (Go, Node 22+, and `redis-server` in `PATH`). CI provides all three
servers, including the FreeRADIUS MySQL, as service containers.

The backend integration suite drives the full HTTP router with real databases
and fake vendor edges (payment gateway, SMS sender, FreeRADIUS writer) plus an
injectable clock. The shared fixtures in `contracts/` are decoded by both the Go
tests and the frontend tests, so the two sides cannot drift apart without a
failing build.

## Login Page (served by RouterOS)

The Hotspot captive portal shows the **Package** list from the backend, sends the
user to the purchase web app, and logs them in with one of three credential modes
a buyer picks from a segmented control: **Kode Akses** (Access Code, the default),
**Nomor HP** (Phone Number + PIN) and **User** (username + password), per
[ADR 0006](docs/adr/0006-three-hotspot-login-modes.md). The status page shows the
remaining **Active Period** and guides **Extend** / buy-again.

The backend answers the two public reads the page needs, with shapes frozen by
the page and pinned by `contracts/`:

| Endpoint | Body |
| --- | --- |
| `GET /api/packages` | `{"data":[{"id","nama","harga","durasiActivePeriod","batasKecepatan"}]}` |
| `GET /api/active-period?phone=…` | `{"data":{"expiry":"<ISO-8601>"}}`, `null` when the number has no Active Period |

An unreachable FreeRADIUS MySQL answers `503` for the Active Period read, which
the status page shows as "tidak dapat dimuat" instead of a wrong expiry.

| Path | Role |
| --- | --- |
| `KOPINETGO-2024/login.html` | Login Page: login form + Package list |
| `KOPINETGO-2024/status.html` | Status page: remaining Active Period + guidance |
| `KOPINETGO-2024/assets/voucher-client.js` | The single client seam (Package list, purchase URL, login, Active Period) |
| `KOPINETGO-2024/config/data/voucher.js` | Deployment configuration (`cfgVoucher`) |
| `tests/unit/` | Vitest + jsdom tests of the seam |
| `tests/smoke/` | Playwright browser tests of the pages |

The pages are served by RouterOS, so `$(...)` template variables
(`$(username)`, `$(chap-id)`, `$(error)`, …) remain in the HTML.

### Auth and SMS configuration

The entry path and the SMS outbox are configured with environment variables
(documented in `.env.example`): `AUTH_MAGIC_LINK_TTL`, `AUTH_SESSION_TTL`,
`AUTH_COOKIE_SECURE`, `AUTH_COOKIE_NAME`, `AUTH_APP_BASE_URL`,
`AUTH_MAGIC_LINK_PATH`, `AUTH_LOGIN_PAGE_ALLOWLIST`,
`AUTH_RATE_LIMIT_PHONE_WINDOW/LIMIT`, `AUTH_RATE_LIMIT_IP_WINDOW/LIMIT`, and
`SMS_DEVICE`, `SMS_BAUD`, `SMS_COMMAND_TIMEOUT`, `SMS_LEASE`,
`SMS_SWEEP_INTERVAL`, `SMS_MAX_ATTEMPTS`, `SMS_RETRY_BACKOFF`. The modem is only
opened by the `worker` process; the `api` process never touches the device.

`AUTH_COOKIE_SECURE` defaults to `true` in `.env.example` and the compose file,
because the deployment terminates TLS in front of the proxy. A local plain-HTTP
setup must set it to `false`, otherwise the browser drops the session cookie.

### Deployment configuration

All deployment values live in `KOPINETGO-2024/config/data/voucher.js`. Nothing
is hardcoded elsewhere.

| Key | Default | Required | Meaning |
| --- | --- | --- | --- |
| `backendBaseUrl` | `""` | **yes** | Backend base URL **including the API prefix**, e.g. `https://hotspot.example/api` (no trailing slash). The backend mounts every endpoint under `/api`. When empty, the Package list shows "not configured" and the Active Period cannot load. |
| `packagesPath` | `/packages` | no | Public Package list endpoint. |
| `activePeriodPath` | `/active-period` | no | Public Active Period endpoint (ADR 0005). |
| `expiryWarningHours` | `24` | no | Hours left before the Active Period counts as "almost over". |
| `appBaseUrl` | `""` | **yes** | Purchase web app base URL. When empty, the "Beli" button is disabled. |
| `packagesFromBackend` | `1` | no | `1` = read Packages from the backend, `0` = use `fallbackPackages`. |
| `fallbackPackages` | `[]` | no | Static list used only when the toggle is `0`. |

### Login modes

The hotspot supports three credential schemes (ADR 0006). They are enabled in
`KOPINETGO-2024/config/data/login.js` (`cfgLogin.modes`), each with a tab label:

```js
defaultMode: "accessCode",
modes: {
  accessCode: { status: 1, label: "Kode Akses" },
  phone: { status: 1, label: "Nomor HP" },
  user: { status: 1, label: "User" },
},
```

`status: 1` enables a mode; a site can ship a single one and no tab is shown, or
enable several and let the buyer pick. This deployment enables all three, so the
segmented control is always visible. `defaultMode` names the mode the form starts
in — this deployment starts on **Kode Akses**, because a buyer leaves the purchase
flow with a code; when it is unset, disabled or unknown the first enabled mode in
canonical order (`phone`, `accessCode`, `user`) wins. The tab order does not
change with the default: `Kode Akses` is the second tab while being the selected
one. The form title, field shape and placeholders come from the mode descriptor in
`KOPINETGO-2024/assets/voucher-client.js`, not from the page markup.

Each mode derives the hotspot credentials differently:

| Mode | Fields | Credentials submitted |
| --- | --- | --- |
| `phone` — Phone Number + PIN | Phone Number + PIN (6 digits, masked) | `username = Phone Number`, `password = CHAP(PIN)` |
| `accessCode` — Access Code | one field | `username = code`, `password = CHAP(code)`; the code is a single secret, so no 6-digit rule applies |
| `user` — User | username + password (masked) | `username` as typed, `password = CHAP(password)`; the password has no 6-digit rule |

A wrong-credential message follows the active mode (for **Access Code** it reads
"Kode Akses salah.", for **User** "Username atau password salah."); the
expired-account message is shared by every mode.

### Package list

The list under the banner is rendered by `KOPINETGO-2024/assets/voucher-client.js`
into `#paketInternet`, on the Login Page and on the Status page (where it is the
Extend / buy-again target). Its heading and subtitle come from `cfgIP.label` in
`KOPINETGO-2024/config/data/internetPackage.js` — the copy the page already
advertises — and the cards are themed by the block at the end of
`KOPINETGO-2024/assets/style.css`: `#f8fafc` surfaces with the same indigo shadow
as the form fields, the `#d21c28` section title, a pill per Package attribute and
the coral pill of the Login button for "Beli". Those colours are the `--pkg-*`
variables in that block, so a deployment rethemes the list by editing variables,
not markup. Bump `assets/style.css?v=` / `assets/voucher-client.js?v=` in the four
pages when either file changes.

### Backend contracts (read-only, public)

Both endpoints are public and read-only; the backend answers them with
`Access-Control-Allow-Origin: *` so the page may call it from the router's own
origin (ADR 0014).

- `GET <backendBaseUrl><packagesPath>` → `{ data: [{ id, nama, harga, durasiActivePeriod, batasKecepatan }] }`
- `GET <backendBaseUrl><activePeriodPath>?phone=<Phone Number>` → `{ data: { expiry } }` (`expiry`: ISO-8601 or epoch seconds) — see [ADR 0005](docs/adr/0005-active-period-endpoint.md).

### Purchase app contract

The "Beli" button links to
`<appBaseUrl>?package=<Package id>&return=<Login Page URL>`; point
`appBaseUrl` at the app's `/beli` route.

`/beli` shows the Package and asks for a **Phone Number** when there is no
session, then offers the closed-payment channels and the payment instructions.
It never trusts the URL: the backend re-validates the Package, and `return` is
only echoed back after it matches the deployment's
`AUTH_LOGIN_PAGE_ALLOWLIST` (ADR 0011/0015).

Checkout endpoints (session cookie required):

| Endpoint | Body |
| --- | --- |
| `GET /api/transactions/channels` | `{"data":[{"code","name","group","fee_flat_idr","fee_percent"}]}` |
| `POST /api/transactions` | `{"package_id","channel","return_url"?}` → the pending Transaction, or the same one that already exists |
| `GET /api/transactions/{id}` | the Transaction the app polls; carries `pin` once it is `provisioned` |
| `POST /api/payments/callback` | public; signed by the gateway, the only path to `paid` |

Fees are shown separately from the Package price, and polling stops at
`provisioned`, `failed` or `expired` — or while the tab is hidden; `paid` is not
the end of the flow, the worker still has to write the account (ADR 0008/0015/
0016). That completion screen shows the **Phone Number**, the **PIN**, the next
step, and the way back to the Login Page only when the checkout carried an
allowed `return` target.

The passwordless entry endpoints (session cookie per ADR 0009/0011):

| Endpoint | Body |
| --- | --- |
| `POST /api/auth/magic-link` | `{"phone_number","package_id","return_url"}` → `202 {"data":{"sent":true}}`, identical for every number |
| `POST /api/auth/magic-link/exchange` | `{"token"}` → `200 {"data":{"phone_number","expires_at","purpose","package_id","return_url"}}` and sets the `HttpOnly` session cookie |
| `GET /api/auth/session` | `200 {"data":{"phone_number","expires_at"}}`, or `401 {"error":{"code":"unauthorized",…}}` |
| `POST /api/auth/logout` | `204`; revokes the session row and clears the cookie |

Fixtures for all four live in `contracts/` and are decoded by both the backend
integration tests and the app's zod schemas.

### Legacy Mikhmon expiry (optional)

`KOPINETGO-2024/config/data/expiredMikhmon.js` (`cfgEM`) keeps the older
Mikhmon expiry iframe on the status page. It is independent of the Active
Period endpoint. Once the backend endpoint is live, set `cfgEM.status` to `0`
to retire it and leave the seam-rendered Active Period panel as the only
expiry display.

### Login Page development

```bash
npm install
npm run typecheck     # tsc --noEmit (jsconfig checkJs on the seam + config)
npm test              # Vitest unit tests
npm run test:smoke    # Playwright browser smoke tests
npm run test:all      # unit + smoke
npm run test:watch    # Vitest in watch mode
```

The smoke tests serve `KOPINETGO-2024/` from a small static server and stub the
backend at the network boundary; no backend is required.
