# The purchase app is a SPA served from the API's origin

The purchase web app is built by SvelteKit as a static SPA (no server-side
rendering, no Node process in production) and served by the same reverse proxy
that forwards `/api` to the Go backend, so browser and API share one origin.
Because there is no server-side render, the app never reads or holds the session:
the opaque session cookie from ADR 0009 lives only between the browser and the
backend. The browser calls `/api/...` relative to its own origin, and the app is
configured for that path rather than an absolute backend URL.

Chosen because ADR 0009 already made the session an `HttpOnly`, server-side
cookie with no token visible to JavaScript, and a SvelteKit server would only
duplicate that: any `+server.ts`/`+page.server.ts` that read the cookie would
become a second holder of session state, with its own lifetime and revocation
story, for no benefit at this scale. A static build also matches how the stack is
deployed — one host that also holds the SMS modem (ADR 0003) — and lets the
reverse proxy be the only component that knows about origins. This is what the
health status screen relies on as well: it reads `GET /api/health` through the
shared axios client bound to `/api`, and renders a degraded backend instead of
failing, with no CORS configuration anywhere.

Consequences:

- All session work happens in the Go backend; the frontend has no auth server
  routes, no cookie parsing and no `hooks.server.ts` session handling.
- Deployment serves `build/` (static files + `index.html` fallback) and proxies
  `/api` to the API container; CI builds the app with `npm run build` and never
  runs it as a server.
- The API base path `/api` is a contract with one sanctioned build-time
  override (`VITE_API_BASE_URL`). Changing it means changing the proxy, the
  client configuration and the ADRs together.
- Any future client (a native mobile app, for example) needs its own auth path,
  because the browser-only cookie flow is not available to it.
- Dev keeps the same shape: Vite proxies `/api` to the local API, so development
  never introduces a second origin that production does not have.

Rejected alternatives: SvelteKit SSR with server-side session reads (a second
session holder with its own revocation surface, plus a Node process to operate);
client-side tokens in memory or `localStorage` (readable by JavaScript, which
ADR 0009 exists to avoid); separate `app.` and `api.` origins with CORS (more
moving parts, and cookie domain problems for zero benefit at this scale).
