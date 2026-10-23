/**
 * Stub for `$app/paths` in Vitest. The SPA is served from the origin root, so
 * SvelteKit's `resolve()` returns the path unchanged; tests only need that
 * behaviour, not the router. Components that link internally import `resolve`
 * (the `svelte/no-navigation-without-resolve` lint rule requires it).
 */
export function resolve(path: string): string {
  return path;
}
