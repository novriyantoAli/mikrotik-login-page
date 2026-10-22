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
make up                   # docker compose up -d --build
open http://localhost:8080/            # purchase app (status screen)
curl http://localhost:8080/api/health   # backend + Postgres + MySQL + Redis
make down
```

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
make lint                 # gofmt + go vet, Prettier
make -C backend lint-golangci  # optional: needs a golangci-lint built for Go 1.26
npm run test:smoke        # Playwright browser tests for the Login Page
```

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
user to the purchase web app, and logs them in with **Phone Number** + **PIN** by
default (other credential modes per [ADR 0006](docs/adr/0006-three-hotspot-login-modes.md)).
The status page shows the remaining **Active Period** and guides **Extend** /
buy-again.

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

### Deployment configuration

All deployment values live in `KOPINETGO-2024/config/data/voucher.js`. Nothing
is hardcoded elsewhere.

| Key | Default | Required | Meaning |
| --- | --- | --- | --- |
| `backendBaseUrl` | `""` | **yes** | Backend base URL (no trailing slash). When empty, the Package list shows "not configured" and the Active Period cannot load. |
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
modes: {
  accessCode: { status: 0, label: "Kode Akses" },
  phone: { status: 1, label: "Nomor HP" },
  user: { status: 0, label: "User" },
},
```

`status: 1` enables a mode. Only **Phone Number + PIN** is enabled by default,
so the Login Page behaves exactly as before and no tab is shown; a segmented
control appears once more than one mode is enabled. The form title, field shape
and placeholders come from the mode descriptor in
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

### Backend contracts (read-only, next slice — PRD build order step 2)

- `GET <backendBaseUrl><packagesPath>` → `{ data: [{ id, nama, harga, durasiActivePeriod, batasKecepatan }] }`
- `GET <backendBaseUrl><activePeriodPath>?phone=<Phone Number>` → `{ data: { expiry } }` (`expiry`: ISO-8601 or epoch seconds) — see [ADR 0005](docs/adr/0005-active-period-endpoint.md).

### Purchase app contract

The "Beli" button links to
`<appBaseUrl>?package=<Package id>&return=<Login Page URL>`.

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
