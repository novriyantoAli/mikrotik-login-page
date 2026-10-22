import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

/**
 * The Login Page side of the two shared fixtures in `contracts/`.
 *
 * The backend integration tests decode the same files through their DTOs, so a
 * field rename on either side turns one of the two suites red instead of
 * silently breaking the page (contracts/README.md). The Login Page itself stays
 * untouched: this file only drives the existing client seam with the fixture.
 */

const require = createRequire(import.meta.url);
const MODULE_PATH = "../../KOPINETGO-2024/assets/voucher-client.js";

function loadModule() {
  delete require.cache[require.resolve(MODULE_PATH)];
  return require(MODULE_PATH);
}

function loadContract(name) {
  const path = resolve(process.cwd(), "contracts", name);
  return JSON.parse(readFileSync(path, "utf8"));
}

function fetchReturning(payload) {
  return vi.fn().mockResolvedValue({ ok: true, json: async () => payload });
}

describe("Login Page contract fixtures", () => {
  beforeEach(() => {
    delete globalThis.cfgVoucher;
    delete globalThis.cfgLogin;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the shared Package fixture through the client seam unchanged", async () => {
    const fixture = loadContract("packages.response.json");
    const { createVoucherClient } = loadModule();
    const client = createVoucherClient({
      config: { packagesFromBackend: 1, backendBaseUrl: "https://api.example", packagesPath: "/packages" },
      fetch: fetchReturning(fixture),
    });

    const packages = await client.fetchPackages();

    expect(fixture.data.length).toBeGreaterThan(0);
    expect(packages).toEqual(
      fixture.data.map((pkg) => ({
        id: pkg.id,
        name: pkg.nama,
        price: pkg.harga,
        activePeriod: pkg.durasiActivePeriod,
        speedLimit: pkg.batasKecepatan,
      })),
    );
  });

  it("turns the shared Active Period fixture into a usable status", async () => {
    const fixture = loadContract("active-period.response.json");
    const { createVoucherClient } = loadModule();
    const client = createVoucherClient({
      config: { backendBaseUrl: "https://api.example", activePeriodPath: "/active-period" },
      fetch: fetchReturning(fixture),
    });

    const account = await client.fetchActivePeriod("08123456789");
    expect(account.expiry).toBe(fixture.data.expiry);

    const expiryMs = Date.parse(fixture.data.expiry);
    const status = client.activePeriodStatus(fixture.data.expiry, {
      now: expiryMs - 2 * 24 * 60 * 60 * 1000,
      warningMs: 24 * 60 * 60 * 1000,
    });

    expect(status.state).toBe("active");
    expect(status.actionLabel).toMatch(/extend/i);
  });
});
