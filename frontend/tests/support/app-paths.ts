/**
 * Stub for `$app/paths` in Vitest. The SPA is served from the origin root, so
 * SvelteKit's `resolve()` returns the path unchanged; tests only need that
 * behaviour, not the router. Dynamic segments are filled from the params so a
 * link such as `resolve('/transaksi/[id]', { id: '12' })` renders as a real href.
 * Components that link internally import `resolve` (the
 * `svelte/no-navigation-without-resolve` lint rule requires it).
 */
export function resolve(path: string, params?: Record<string, string>): string {
  if (!params) {
    return path;
  }
  return path.replace(/\[([^\]]+)\]/g, (segment, key: string) => params[key] ?? segment);
}
