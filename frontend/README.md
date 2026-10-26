# Purchase app (SvelteKit SPA)

SvelteKit + Svelte 5 runes + TypeScript (strict) + Tailwind v4 + shadcn-svelte,
with one axios client and TanStack Query. Served as static files from the same
origin as the API (ADR 0013); no SSR process holds a session.

## Layout

| Path                        | Role                                                                                               |
| --------------------------- | -------------------------------------------------------------------------------------------------- |
| `src/routes/`               | Thin route adapters (`+layout.svelte`, `+page.svelte`)                                             |
| `src/lib/api/client.ts`     | The single axios instance + one normalised error shape                                             |
| `src/lib/config/env.ts`     | Typed wrapper over the build-time `VITE_*` values                                                  |
| `src/lib/domains/<domain>/` | Vertical slice: `schemas/` (zod), `api/`, `queries/`, optional `state/`, `components/`, `index.ts` |
| `src/lib/components/ui/`    | shadcn-svelte primitives (vendeored source, no business logic)                                     |
| `tests/unit/`               | Vitest + Testing Library tests against the shared contract fixtures                                |

Layering is one direction only: `route → component → query → api → axios
client`. Components never import axios; every response is parsed by the domain's
zod schema before it reaches a component.

The primitives in `src/lib/components/ui/` (`card`, `badge`, `button`) were
added from the shadcn-svelte registry source rather than by running the
interactive `shadcn-svelte init` CLI (which requires a design-system preset).
`components.json` is present, so re-running the CLI later regenerates them with
the same aliases; keep business logic out of that directory.

## Routes

| Route                                        | Purpose                                                   |
| -------------------------------------------- | --------------------------------------------------------- |
| `/`                                          | System status (health)                                    |
| `/masuk`                                     | Phone Number → Magic Link request                         |
| `/masuk/tautan`                              | Redeem the Magic Link; a checkout link lands on `/beli`   |
| `/beli?package=<id>&return=<Login Page URL>` | Checkout: Package, payment channel, instructions, polling |

Domains: `packages` (public catalog), `auth` (Magic Link + session), `checkout`
(channels, Transaction, instructions, polling), `health` (system status).

Checkout polls one Transaction every 4s until it settles, and never while the
tab is hidden (`refetchIntervalInBackground: false`); the gateway callback is
still the only authority for settlement (ADR 0008/0015). The `return` target is
rendered from the Transaction the backend validated, never straight from the
address bar.

## Configuration

`.env.example` documents every build-time value. `VITE_API_BASE_URL` defaults to
`/api`, the path the reverse proxy forwards to the backend on the same origin —
there is no CORS and no token in JavaScript.

### Content-Security-Policy

The app ships its own policy in a `<meta>` tag (`kit.csp`, hash mode in
`svelte.config.js`) because SvelteKit emits one inline bootstrap script whose
hash changes with every build; a static header cannot know it. `build/index.html`
carries the matching `sha256-…`, so the reverse proxy must **not** send a stricter
`default-src`/`script-src` header for documents — the two policies intersect, and
a header without the hash leaves a blank page with no failed request. The proxy in
`docker/proxy/nginx.conf` therefore sends only `frame-ancestors 'none'`, the part a
`<meta>` tag cannot express.

## Commands

```bash
npm ci
npm run dev         # Vite dev server; /api is proxied to BACKEND_ORIGIN
npm run build       # static build in build/
npm run typecheck   # svelte-check (strict)
npm test            # Vitest
npm run lint        # ESLint (layering rules) + Prettier check
npm run format      # Prettier write
```

`eslint.config.js` enforces the architecture rules that types cannot: components
may not import `axios` (only the shared client may), and another domain is only
reachable through its `index.ts` barrel.

`BACKEND_ORIGIN` (default `http://127.0.0.1:8080`) only affects the dev server
proxy; the deployed origin is the reverse proxy. It is documented in
`.env.example`.

## Deliberate deviations from the frontend architecture standard

- **npm instead of pnpm.** Repo-wide convention: the root tooling and the Login
  Page use npm, and one package manager keeps `make install` predictable.
- **`lib/config/env.ts` reads Vite's `import.meta.env`** instead of SvelteKit's
  `$env/*`. The app is a static SPA served by nginx (ADR 0013), so there is no
  server environment at runtime, and `$env/static/public` would fail the build
  unless every `PUBLIC_*` value is provisioned at build time. `env.ts` is the
  only file allowed to read `import.meta.env`; domain code imports `env`.
- **Playwright e2e for this app** arrives with issue #17 (hardening & end-to-end
  smoke); the prefactor covers Vitest, component and shared-contract tests.

## Shared contract fixtures

`tests/support/contracts.ts` reads `../../../contracts/*.json`, the same files the
backend integration tests decode. A contract change that only updates one side
fails that side's suite.
