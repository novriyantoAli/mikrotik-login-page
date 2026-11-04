import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/**
 * The panel is a second SPA, mounted under `/admin` on the same origin as the
 * API (ADR 0028). It has no SSR and holds no session of its own: the admin
 * session is an HttpOnly cookie the backend reads, scoped to `/api/admin`.
 *
 * `paths.base` is what makes the two apps impossible to mix up: every asset and
 * every in-app link carries the `/admin` prefix, so the shell can never request
 * `/@vite/...` at the origin root and be answered by the purchase app.
 *
 * @type {import('@sveltejs/kit').Config}
 */
const config = {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({ fallback: 'index.html' }),
    paths: {
      base: '/admin',
    },
    // The panel ships its own policy because SvelteKit emits one inline
    // bootstrap script whose hash changes with every build (same reasoning as
    // the purchase app). The proxy must not send a second, stricter document
    // policy: two policies intersect.
    csp: {
      mode: 'hash',
      directives: {
        'default-src': ['self'],
        'script-src': ['self'],
        // Components set inline style attributes; a hash cannot cover those.
        'style-src': ['self', 'unsafe-inline'],
        'img-src': ['self', 'data:'],
        'connect-src': ['self'],
        'frame-ancestors': ['none'],
        'base-uri': ['none'],
        'form-action': ['self'],
      },
    },
    alias: {
      $lib: 'src/lib',
    },
  },
};

export default config;
