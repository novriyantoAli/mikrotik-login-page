import tailwindcss from '@tailwindcss/vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import path from 'node:path';
import { defineConfig } from 'vitest/config';

// Component and API tests run in jsdom. The axios adapter is replaced per test
// with fixtures from contracts/ (see tests/support), so no backend is needed.
export default defineConfig({
  plugins: [tailwindcss(), svelte()],
  resolve: {
    alias: {
      $lib: path.resolve('./src/lib'),
      // `$app/paths` only exists inside SvelteKit's build; resolve() is a no-op
      // for a root-served SPA, which is all the component tests need.
      '$app/paths': path.resolve('./tests/support/app-paths.ts'),
    },
    conditions: ['browser'],
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['tests/**/*.test.ts'],
    setupFiles: ['tests/setup.ts'],
    css: false,
    restoreMocks: true,
  },
});
