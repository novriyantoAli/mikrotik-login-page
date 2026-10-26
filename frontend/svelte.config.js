import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/**
 * The purchase app is a SPA served by the same origin as the API (ADR 0013):
 * static files plus an index.html fallback, no server-side render holding a
 * session. The session lives only in an HttpOnly cookie the backend reads.
 *
 * @type {import('@sveltejs/kit').Config}
 */
const config = {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({ fallback: 'index.html' }),
    // SvelteKit emits one inline bootstrap script; the reverse proxy's CSP cannot
    // hash it because the hash changes with every build. Hash mode makes the app
    // ship its own policy, so the proxy can stop guessing for documents.
    csp: {
      mode: 'hash',
      directives: {
        'default-src': ['self'],
        'script-src': ['self'],
        // Components set inline style attributes; a hash cannot cover those.
        'style-src': ['self', 'unsafe-inline'],
        // A payment QR is an image on the vendor's host, so https stays open; the
        // rest is own-origin only.
        'img-src': ['self', 'data:', 'https:'],
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
