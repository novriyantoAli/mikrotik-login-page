// SPA mode (ADR 0013): no SSR, so no server-side process ever holds a session.
// The app is prerendered as a shell and the SvelteKit session cookie stays an
// HttpOnly cookie between the browser and the backend.
export const ssr = false;
export const prerender = false;
