/**
 * Centralized configuration for the voucher purchase flow.
 *
 * Deployment-specific values live here and nowhere else. The voucher client
 * module (`assets/voucher-client.js`) reads this object as its default
 * configuration; individual clients can still be created with explicit
 * overrides via `VoucherClient.createVoucherClient({ config: {...} })`.
 *
 * Keep this file free of behavior — it is data only.
 */
var cfgVoucher = {
  // Base URL of the backend that serves the Package list (no trailing slash).
  backendBaseUrl: "",

  // Path of the public Package list endpoint on the backend.
  packagesPath: "/packages",

  // Base URL of the purchase web app (Svelte). The "Beli" button links here.
  appBaseUrl: "",

  // Feature toggle: 1 = read the Package list from the backend,
  // 0 = keep the existing static list (graceful degradation).
  packagesFromBackend: 0,

  // Static Package list used when the backend list is disabled or unreachable.
  // Left empty on purpose until the backend Package-list slice defines the
  // deployment fallback shape.
  fallbackPackages: [],
};
