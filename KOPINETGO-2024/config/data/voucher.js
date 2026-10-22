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
  // Required when packagesFromBackend is 1; leave empty to disable temporarily.
  backendBaseUrl: "",

  // Path of the public Package list endpoint on the backend.
  packagesPath: "/packages",

  // Base URL of the purchase web app (Svelte). The "Beli" button links here.
  appBaseUrl: "",

  // Feature toggle: 1 = read the Package list from the backend,
  // 0 = keep the existing static list (graceful degradation).
  packagesFromBackend: 1,

  // Static Package list used only when the backend list is disabled (toggle 0).
  // Prefer configuring `backendBaseUrl`/`packagesPath` over this fallback.
  // Accepts the backend keys { id, nama, harga, durasiActivePeriod,
  // batasKecepatan } and the normalized { id, name, price, activePeriod,
  // speedLimit }.
  fallbackPackages: [],
};
