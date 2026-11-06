# Admin Panel (SvelteKit SPA)

SvelteKit + Svelte 5 runes + TypeScript (strict) + Tailwind v4 + shadcn-svelte,
with one axios client and TanStack Query. A second SPA (ADR 0028), served under
`/admin` on the same origin as the API; no SSR process holds a session.

The panel is separate from the purchase app on purpose: it is mounted at
`kit.paths.base = '/admin'`, its session is a different HttpOnly cookie scoped to
`/api/admin`, and its CSP is its own.

## Layout

| Path                         | Role                                                                                               |
| ---------------------------- | -------------------------------------------------------------------------------------------------- |
| `src/routes/`                | Thin route adapters (`/` home + login, `/paket`, `/transaksi`, `/admin`)                           |
| `src/lib/api/client.ts`      | The single axios instance, one normalised error shape, the 401-notification seam                   |
| `src/lib/config/env.ts`      | Typed wrapper over the build-time `VITE_*` values                                                  |
| `src/lib/domains/<domain>/`  | Vertical slice: `schemas/` (zod), `api/`, `queries/`, optional `state/`, `components/`, `index.ts` |
| `src/lib/components/ui/`     | shadcn-svelte primitives (vendored source, no business logic)                                      |
| `src/lib/components/shared/` | Composed cross-domain pieces (`AppShell`, `PageHeader`, `EmptyState`)                              |
| `tests/unit/`                | Vitest + Testing Library tests against the shared contract fixtures                                |

Layering is one direction only: `route → component → query → api → axios
client`. Components never import axios; every response is parsed by the domain's
zod schema before it reaches a component, and another domain is only reachable
through its `index.ts` barrel (`eslint.config.js` enforces both, alongside a ban
on `axios` imports outside the shared client).

Domains:

- `auth` — admin sign-in, the session probe, sign-out, and changing your own
  password. A 401 from any other screen is turned into "session expired" and the
  login form, never an empty page.
- `packages` — the catalog including retired Packages, create/edit with the
  field rules mirrored from the backend, and activate/deactivate (the last
  active Package cannot be retired; the server answers 409).
- `transactions` — the panel's Transaction search: state, Phone Number and a
  date range, with the shared `{data, total_count, page, page_size}` pagination.
  Each row shows the Package name and price the buyer **bought** (the snapshot on
  the Transaction, ADR 0026), the provisioning attempt count and the last SMS
  outcome. No row carries a PIN, and there is no reveal action here.
- `admins` — owner-only team management: list, create, reset password,
  activate/deactivate. The UI hides what a role may not do; the server enforces
  it.

## Routes

| Route              | Purpose                                                     |
| ------------------ | ----------------------------------------------------------- |
| `/admin`           | Shell + login; signed in, the home card and password change |
| `/admin/paket`     | Package catalog, edit form (owner), activate/deactivate     |
| `/admin/transaksi` | Transaction search for every role, without the PIN          |
| `/admin/admin`     | Team management (owner only)                                |

Routes are thin: the domain components own queries, loading/error/empty states
and forms.

## Configuration

`.env.example` documents every build-time value. `VITE_API_BASE_URL` defaults to
`/api`, the same-origin path the reverse proxy forwards to the backend. There is
no token in JavaScript: the admin session is an HttpOnly cookie.

The app ships its own content policy (`kit.csp`, hash mode) because SvelteKit
emits an inline bootstrap script whose hash changes with every build. The reverse
proxy must not send a stricter document policy for `/admin`; the two policies
intersect.

## Commands

```bash
npm ci
npm run dev         # Vite dev server; /api is proxied to BACKEND_ORIGIN
npm run build       # static build in build/
npm run check       # svelte-check (strict)
npm test            # Vitest
npm run lint        # ESLint (layering rules) + Prettier check
npm run format      # Prettier write
```

`BACKEND_ORIGIN` (default `http://127.0.0.1:8080`) only affects the dev server
proxy; the deployed origin is the reverse proxy.

## Deployment (ADR 0028)

The panel is its own image and compose service (`admin`), built from this
directory by `admin/Dockerfile` and served as static files by nginx:

- the proxy forwards `/admin` unchanged and this container strips that one prefix,
  so the shell's `/admin/_app/...` asset URLs resolve from its own root; a deep
  link such as `/admin/paket` falls back to `index.html`;
- the shell is `Cache-Control: no-store` (a stale shell asks for asset hashes the
  current build has already replaced) while `_app/immutable/` is cached for a
  year;
- the container states its own `frame-ancestors 'none'`; the document policy
  itself comes from the build's `<meta>` tag, so the proxy must not send a second,
  stricter one;
- `ADMIN_TAG` selects the image tag, so the panel is released independently of the
  purchase app.

`docker compose up -d --build` serves it at `http://localhost:8080/admin`, next to
the purchase app at `/` and the API at `/api`.

## Shared contract fixtures

`tests/support/contracts.ts` reads `../../../contracts/*.json`, the same files the
backend integration tests decode. `admin-login.request.json`,
`admin-session.response.json`, `admin-admins.response.json`,
`admin-package-create.request.json`, `admin-package-update.request.json` and
`admin-packages.response.json` are the panel's contract; changing one side alone
fails the other side's suite.

## Deliberate deviations from the frontend architecture standard

- **npm instead of pnpm.** Repo-wide convention: the root tooling, the Login Page
  and the purchase app use npm, so `make install` stays predictable.
- **`lib/config/env.ts` reads Vite's `import.meta.env`** instead of SvelteKit's
  `$env/*`. The app is a static SPA served by nginx, so there is no server
  environment at runtime. `env.ts` is the only file allowed to read
  `import.meta.env`.
- **No Playwright suite inside this package.** The panel's browser journey lives
  with the other end-to-end specs (`tests/e2e/admin-panel.spec.mjs`, Playwright
  project `panel`) because it needs the deployed stack — the proxy, the panel's
  own image, the real session cookie — not a dev server. This package stays
  Vitest, component and shared-contract tests.
