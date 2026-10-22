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
    alias: {
      $lib: 'src/lib',
    },
  },
};

export default config;
