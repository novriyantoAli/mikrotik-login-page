# The Admin Panel is its own app on its own image

ADR 0013 made the purchase app a SPA on the same origin as the API, with the
session living only in an HttpOnly cookie. The **Admin Panel** is a third
audience on that origin: different people, different session, different risk.

Putting it in the same SvelteKit app is the cheapest option and it was the first
one on the table. Three things pushed it out:

- The two audiences need different headers. The purchase app ships a CSP tuned
  for a payment QR image (`img-src https:`) and an inline-style allowance; the
  panel needs none of that. One document policy serving both means one of them is
  looser than it should be.
- The panel and the purchase app have no shared release cadence. A panel bug
  fixed tonight should not require re-releasing the flow a buyer is in the middle
  of, and a checkout change should not be able to regress the tool an operator
  uses during an incident.
- Isolation that exists only as a route group is a convention, not a boundary.
  A second artefact is a boundary: its own build, its own image tag, its own
  static root.

## Decision

**1. `admin/` is a second SvelteKit app in the monorepo**, stack identical to
`frontend/` (Svelte 5, Tailwind v4, shadcn-svelte, zod, TanStack Query, axios)
and laid out per the `frontend-ddd-sveltekit` skill:
`src/lib/domains/{auth,packages,transactions,subscribers,audit,admins}` with an
`index.ts` barrel as the only outward surface.

**2. It is built and shipped as its own image and compose service (`admin`)**,
and the proxy routes `/admin` to it with an SPA fallback on its own `index.html`
and `Cache-Control: no-store` for that shell. The purchase app keeps `/` and the
backend keeps `/api`; nothing about their deployment changes.

**3. The panel is a SPA too** (`ssr = false`), for the same reason as ADR 0013:
no server-side process should hold an admin session. The cookie's
`Path=/api/admin` (ADR 0024) is what scopes it; the app's own path is irrelevant
to the browser's cookie matching.

**4. The panel gets its own CSP and headers**, stated by its own container: no
`https:` image sources, `frame-ancestors 'none'`, and a document policy that
does not have to accommodate a vendor QR.

**5. The panel is desktop-first.** The purchase app is held to a 360px standard
because a buyer is on a phone; an operator working a list of Transactions is not.
The 360px Playwright project stays a property of `frontend/`.

**6. CI and the Makefile gain a matching surface**: typecheck, lint, unit tests
and build for `admin/`, plus its own Playwright project. A panel that only builds
on the machine it was written on is not delivered.

## Rejected alternatives

- **A route group inside `frontend/`.** One build, one deployment, and the
  headers, the release cadence and the isolation all become conventions again.
  SvelteKit's per-route code splitting would keep the panel's code out of the
  buyer's initial load, so bundle size was never the argument.
- **A separate subdomain.** It separates cookies and origins more thoroughly, and
  it adds DNS, certificate and CORS surface to a deployment that deliberately
  settled on one origin (ADR 0009).
- **Build the panel separately but stack it into the existing `app` image.** Two
  static roots in one nginx serves the files, and it removes exactly the benefit
  that justified splitting the app.
- **No web UI at all — a CLI.** Cheapest to build for an operator who is
  comfortable with a terminal, and it throws away the two things this panel is
  for: looking at a list, and being usable by someone who is not.

## Consequences

- Deployment gains one service and one image tag; `docker-compose.yml`,
  `.env.example`, the README and CI all gain an entry.
- The panel's zod schemas mirror the admin DTOs, and the shared fixtures in
  `contracts/` are what keep Go and TypeScript from drifting — the same mechanism
  the buyer-facing contracts already use.
- Two SvelteKit apps mean two `npm install` trees and two `pnpm`/`npm` caches in
  CI. Accepted: the alternative is a shared workspace package, which is more
  machinery than two apps of this size deserve.
- A panel session and a buyer session can coexist in one browser without
  interference: different cookie names, different paths, different tables.
