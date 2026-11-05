/**
 * Stub for `$app/paths` in Vitest. The panel is mounted at `/admin`
 * (`kit.paths.base`), so `resolve()` prefixes absolute paths exactly like the
 * deployed shell does. Tests only need that behaviour, not the router.
 */
const base = '/admin';

export function resolve(path: string, params?: Record<string, string>): string {
  const resolved = path === '/' ? base : base + (path.startsWith('/') ? path : `/${path}`);

  if (!params) {
    return resolved;
  }
  return resolved.replace(/\[([^\]]+)\]/g, (segment, key: string) => params[key] ?? segment);
}
