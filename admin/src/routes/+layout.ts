// SPA mode (ADR 0028): no SSR, so no server-side process ever holds a session.
// The panel is a static shell served under /admin; the admin session stays an
// HttpOnly cookie between the browser and the backend.
export const ssr = false;
export const prerender = false;
