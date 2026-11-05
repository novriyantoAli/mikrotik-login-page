/**
 * Typed configuration wrapper. Domain code imports `env` and never reads
 * `import.meta.env` directly.
 *
 * The panel is a static SPA (ADR 0028), so there is no server-side environment
 * to read at runtime: everything here is baked in at build time. `apiBaseUrl`
 * defaults to `/api`, the path the reverse proxy forwards to the backend on the
 * same origin — a contract, not a deployment value.
 */
function readString(value: unknown, fallback: string): string {
  return typeof value === 'string' && value.trim().length > 0 ? value.trim() : fallback;
}

function readPositiveInt(value: unknown, fallback: number): number {
  const parsed = typeof value === 'string' ? Number.parseInt(value, 10) : Number.NaN;
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

export const env = {
  /** Base URL of the backend API, same origin by default. */
  apiBaseUrl: readString(import.meta.env.VITE_API_BASE_URL, '/api'),
  /** Request timeout for one API call. */
  requestTimeoutMs: readPositiveInt(import.meta.env.VITE_REQUEST_TIMEOUT_MS, 15_000),
} as const;
