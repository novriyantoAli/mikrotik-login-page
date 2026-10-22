# Purchase app (SvelteKit SPA)

SvelteKit + Svelte 5 runes + TypeScript (strict) + Tailwind v4 + shadcn-svelte,
with one axios client and TanStack Query. Served as static files from the same
origin as the API (ADR 0013); no SSR process holds a session.

## Layout

| Path                        | Role                                                                            |
| --------------------------- | ------------------------------------------------------------------------------- |
| `src/routes/`               | Thin route adapters (`+layout.svelte`, `+page.svelte`)                          |
| `src/lib/api/client.ts`     | The single axios instance + one normalised error shape                          |
| `src/lib/config/env.ts`     | Typed wrapper over the build-time `VITE_*` values                               |
| `src/lib/domains/<domain>/` | Vertical slice: `schemas/` (zod), `api/`, `queries/`, `components/`, `index.ts` |
| `src/lib/components/ui/`    | shadcn-svelte primitives (vendeored source, no business logic)                  |
| `tests/unit/`               | Vitest + Testing Library tests against the shared contract fixtures             |

Layering is one direction only: `route → component → query → api → axios
client`. Components never import axios; every response is parsed by the domain's
zod schema before it reaches a component.

The primitives in `src/lib/components/ui/` (`card`, `badge`, `button`) were
added from the shadcn-svelte registry source rather than by running the
interactive `shadcn-svelte init` CLI (which requires a design-system preset).
`components.json` is present, so re-running the CLI later regenerates them with
the same aliases; keep business logic out of that directory.

## Configuration

`.env.example` documents every build-time value. `VITE_API_BASE_URL` defaults to
`/api`, the path the reverse proxy forwards to the backend on the same origin —
there is no CORS and no token in JavaScript.

## Commands

```bash
npm ci
npm run dev         # Vite dev server; /api is proxied to BACKEND_ORIGIN
npm run build       # static build in build/
npm run typecheck   # svelte-check (strict)
npm test            # Vitest
npm run lint        # Prettier check
```

`BACKEND_ORIGIN` (default `http://127.0.0.1:8080`) only affects the dev server
proxy; the deployed origin is the reverse proxy. It is documented in
`.env.example`.

`npm run lint` currently runs Prettier only; ESLint (with the layering rules the
architecture expects) is not wired yet, so typechecking and the strict layering
review carry that job. This is the one frontend standard not machine-enforced in
the prefactor.

## Shared contract fixtures

`tests/support/contracts.ts` reads `../../../contracts/*.json`, the same files the
backend integration tests decode. A contract change that only updates one side
fails that side's suite.
