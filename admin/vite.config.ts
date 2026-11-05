import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// In development the panel still calls /api on its own origin: Vite forwards it
// to the local API, exactly like the reverse proxy does in production. The app
// itself is mounted under /admin by `kit.paths.base`.
const backendOrigin = process.env.BACKEND_ORIGIN ?? 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [tailwindcss(), sveltekit()],
  server: {
    proxy: {
      '/api': {
        target: backendOrigin,
        changeOrigin: true,
      },
    },
  },
});
