import { describe, it, expect, beforeEach, vi } from "vitest";
import { createRequire } from "node:module";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import vm from "node:vm";

const require = createRequire(import.meta.url);
const MODULE_PATH = "../../KOPINETGO-2024/assets/voucher-client.js";
const MD5_PATH = resolve(process.cwd(), "KOPINETGO-2024/assets/md5.js");

/** Load a fresh copy of the module so config reads are not cached between tests. */
function loadModule() {
  delete require.cache[require.resolve(MODULE_PATH)];
  return require(MODULE_PATH);
}

/** Independent source of truth for expected MD5 output. */
function expectedMd5(input) {
  return createHash("md5").update(input).digest("hex");
}

/** Load the repo's CHAP MD5 helper into the global scope the same way the page does. */
function loadRepoMd5() {
  const source = readFileSync(MD5_PATH, "utf8");
  vm.runInThisContext(source);
  return globalThis.hexMD5;
}

describe("voucher client seam", () => {
  beforeEach(() => {
    delete globalThis.cfgVoucher;
  });

  it("exposes a single public interface", () => {
    const VoucherClient = loadModule();
    expect(typeof VoucherClient.createVoucherClient).toBe("function");
    expect(typeof VoucherClient.resolveConfig).toBe("function");
    expect(typeof VoucherClient.client.fetchPackages).toBe("function");
    expect(typeof VoucherClient.client.loadPackages).toBe("function");
    expect(typeof VoucherClient.client.renderPackages).toBe("function");
    expect(typeof VoucherClient.client.buildPurchaseUrl).toBe("function");
    expect(typeof VoucherClient.client.hashPin).toBe("function");
    expect(typeof VoucherClient.client.login).toBe("function");
    expect(typeof VoucherClient.client.describeError).toBe("function");
  });

  it("reads centralized config (backend URL, app URL, toggle) from the global cfgVoucher", () => {
    globalThis.cfgVoucher = {
      backendBaseUrl: "https://api.kopinetgo.example",
      appBaseUrl: "https://app.kopinetgo.example/beli",
      packagesFromBackend: 1,
    };

    const { resolveConfig } = loadModule();
    const config = resolveConfig();

    expect(config.backendBaseUrl).toBe("https://api.kopinetgo.example");
    expect(config.appBaseUrl).toBe("https://app.kopinetgo.example/beli");
    expect(config.packagesFromBackend).toBe(1);
  });

  it("falls back to safe defaults when no config is present", () => {
    const { resolveConfig } = loadModule();
    const config = resolveConfig();

    expect(config.backendBaseUrl).toBe("");
    expect(config.appBaseUrl).toBe("");
    expect(config.packagesFromBackend).toBe(0);
  });

  it("lets explicit overrides win over the global config and normalizes the toggle/path", () => {
    globalThis.cfgVoucher = { appBaseUrl: "https://from-config.example", packagesFromBackend: 1 };
    const { resolveConfig } = loadModule();

    const config = resolveConfig({ appBaseUrl: "https://override.example", packagesFromBackend: 0, packagesPath: "v2/packages" });

    expect(config.appBaseUrl).toBe("https://override.example");
    expect(config.packagesFromBackend).toBe(0);
    expect(config.packagesPath).toBe("/v2/packages");
  });

  describe("fetchPackages", () => {
    it("returns the normalized static fallback when the backend list is disabled", async () => {
      const { createVoucherClient } = loadModule();
      const fetchMock = vi.fn();
      const client = createVoucherClient({
        config: {
          packagesFromBackend: 0,
          fallbackPackages: [{ id: 7, nama: "Paket Harian", harga: "Rp 5.000", durasiActivePeriod: "1 Hari", batasKecepatan: "10M" }],
        },
        fetch: fetchMock,
      });

      const packages = await client.fetchPackages();

      expect(fetchMock).not.toHaveBeenCalled();
      expect(packages).toEqual([{ id: 7, name: "Paket Harian", price: "Rp 5.000", activePeriod: "1 Hari", speedLimit: "10M" }]);
    });

    it("fetches the Package list from the configured backend endpoint and normalizes it", async () => {
      const { createVoucherClient } = loadModule();
      const payload = { data: [{ id: 1, nama: "Paket 12 Jam", harga: "Rp 3.000", durasiActivePeriod: "12 Jam", batasKecepatan: "5M" }] };
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => payload });
      const client = createVoucherClient({
        config: { packagesFromBackend: 1, backendBaseUrl: "https://api.example/", packagesPath: "/packages" },
        fetch: fetchMock,
      });

      const packages = await client.fetchPackages();

      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(fetchMock.mock.calls[0][0]).toBe("https://api.example/packages");
      expect(packages).toEqual([{ id: 1, name: "Paket 12 Jam", price: "Rp 3.000", activePeriod: "12 Jam", speedLimit: "5M" }]);
    });

    it("rejects with a coded error when the backend responds with a failure status", async () => {
      const { createVoucherClient, VoucherError } = loadModule();
      const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 503, json: async () => ({}) });
      const client = createVoucherClient({
        config: { packagesFromBackend: 1, backendBaseUrl: "https://api.example" },
        fetch: fetchMock,
      });

      await expect(client.fetchPackages()).rejects.toBeInstanceOf(VoucherError);
      await expect(client.fetchPackages()).rejects.toMatchObject({ code: "http_error" });
    });

    it("rejects with a coded error when the backend is unreachable", async () => {
      const { createVoucherClient } = loadModule();
      const fetchMock = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
      const client = createVoucherClient({
        config: { packagesFromBackend: 1, backendBaseUrl: "https://api.example" },
        fetch: fetchMock,
      });

      await expect(client.fetchPackages()).rejects.toMatchObject({ code: "network_error" });
    });

    it("rejects when the backend list is enabled but no base URL is configured", async () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { packagesFromBackend: 1, backendBaseUrl: "" }, fetch: vi.fn() });

      await expect(client.fetchPackages()).rejects.toMatchObject({ code: "config_missing" });
    });
  });

  describe("buildPurchaseUrl", () => {
    it("carries the chosen Package identity and the return target", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { appBaseUrl: "https://app.example/beli" } });

      const url = client.buildPurchaseUrl({ id: 42 }, { returnUrl: "http://hotspot.local/login?mac=AA:BB" });

      expect(url).toBe("https://app.example/beli?package=42&return=" + encodeURIComponent("http://hotspot.local/login?mac=AA:BB"));
    });

    it("appends to a base URL that already has a query string", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { appBaseUrl: "https://app.example/beli?src=hotspot" } });

      const url = client.buildPurchaseUrl({ id: "abc" }, { returnUrl: "http://hotspot.local/login" });

      expect(url).toBe("https://app.example/beli?src=hotspot&package=abc&return=" + encodeURIComponent("http://hotspot.local/login"));
    });

    it("keeps query parameters before a fragment in the app URL", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { appBaseUrl: "https://app.example/#/beli" } });

      const url = client.buildPurchaseUrl({ id: 9 }, { returnUrl: "http://hotspot.local/login" });

      expect(url).toBe("https://app.example/?package=9&return=" + encodeURIComponent("http://hotspot.local/login") + "#/beli");
    });

    it("rejects a Package without an identity", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { appBaseUrl: "https://app.example/beli" } });

      expect(() => client.buildPurchaseUrl({ name: "no id" })).toThrow(/identity/i);
    });

    it("rejects when no purchase app URL is configured", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { appBaseUrl: "" } });

      expect(() => client.buildPurchaseUrl({ id: 1 })).toThrow(/app base url/i);
    });
  });

  describe("renderPackages", () => {
    const packages = [
      { id: 1, nama: "Paket Harian", harga: "Rp 5.000", durasiActivePeriod: "1 Hari", batasKecepatan: "10M" },
      { id: 2, nama: "Paket Bulanan", harga: "Rp 100.000", durasiActivePeriod: "30 Hari", batasKecepatan: "20M" },
    ];

    it("renders name, price, Active Period, speed limit and a Beli button per Package", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { appBaseUrl: "https://app.example/beli" }, document });
      const container = document.createElement("div");

      client.renderPackages(container, packages);

      const cards = container.querySelectorAll(".package-card");
      expect(cards).toHaveLength(2);

      const first = cards[0].textContent;
      expect(first).toContain("Paket Harian");
      expect(first).toContain("Rp 5.000");
      expect(first).toContain("1 Hari");
      expect(first).toContain("10M");

      const buyButtons = container.querySelectorAll("button");
      expect(buyButtons).toHaveLength(2);
      expect(buyButtons[0].textContent).toBe("Beli");
    });

    it("shows an informative empty state when there are no Packages", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ document });
      const container = document.createElement("div");

      client.renderPackages(container, []);

      expect(container.textContent).toMatch(/belum ada paket/i);
      expect(container.querySelectorAll("button")).toHaveLength(0);
    });

    it("navigates to the purchase URL when Beli is pressed", () => {
      const { createVoucherClient } = loadModule();
      const navigate = vi.fn();
      const client = createVoucherClient({
        config: { appBaseUrl: "https://app.example/beli" },
        document,
        navigate,
      });
      const container = document.createElement("div");
      client.renderPackages(container, [packages[0]]);

      container.querySelector("button").click();

      expect(navigate).toHaveBeenCalledTimes(1);
      const url = new URL(navigate.mock.calls[0][0]);
      expect(url.searchParams.get("package")).toBe("1");
      expect(url.searchParams.get("return")).toBe(window.location.href);
    });

    it("disables Beli without navigating or throwing when the purchase app is not configured", () => {
      const { createVoucherClient } = loadModule();
      const navigate = vi.fn();
      const client = createVoucherClient({ config: { appBaseUrl: "" }, document, navigate });
      const container = document.createElement("div");
      client.renderPackages(container, [packages[0]]);

      const button = container.querySelector("button");
      expect(button.disabled).toBe(true);

      expect(() => button.click()).not.toThrow();
      expect(navigate).not.toHaveBeenCalled();
    });

    it("explains a Package without an identity instead of blaming the configuration", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { appBaseUrl: "https://app.example/beli" }, document });
      const container = document.createElement("div");
      client.renderPackages(container, [{ name: "no id", price: "Rp 0", activePeriod: "1 Hari", speedLimit: "1M" }]);

      const button = container.querySelector("button");
      expect(button.disabled).toBe(true);
      expect(button.getAttribute("title")).toMatch(/paket ini belum dapat dibeli/i);
    });
  });

  describe("loadPackages", () => {
    const backendConfig = { packagesFromBackend: 1, backendBaseUrl: "https://api.example" };
    const backendPayload = {
      data: [{ id: 1, nama: "Paket Harian", harga: "Rp 5.000", durasiActivePeriod: "1 Hari", batasKecepatan: "10M" }],
    };

    it("shows a loading indicator, then renders the Packages fetched from the backend", async () => {
      const { createVoucherClient } = loadModule();
      let respond;
      const fetchMock = vi.fn().mockReturnValue(new Promise((resolve) => {
        respond = resolve;
      }));
      const client = createVoucherClient({ config: backendConfig, fetch: fetchMock, document });
      const container = document.createElement("div");

      const pending = client.loadPackages(container);
      expect(container.textContent).toMatch(/memuat paket/i);

      respond({ ok: true, json: async () => backendPayload });
      const result = await pending;

      expect(result.status).toBe("success");
      expect(result.packages).toHaveLength(1);
      expect(container.textContent).toContain("Paket Harian");
      expect(container.textContent).toContain("Rp 5.000");
      expect(container.textContent).toContain("1 Hari");
      expect(container.textContent).toContain("10M");
    });

    it("shows an informative empty state when the backend has no Packages", async () => {
      const { createVoucherClient } = loadModule();
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ data: [] }) });
      const client = createVoucherClient({ config: backendConfig, fetch: fetchMock, document });
      const container = document.createElement("div");

      const result = await client.loadPackages(container);

      expect(result.status).toBe("empty");
      expect(container.textContent).toMatch(/belum ada paket/i);
    });

    it("shows a clear error state and resolves without throwing when the backend is unreachable", async () => {
      const { createVoucherClient } = loadModule();
      const fetchMock = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
      const client = createVoucherClient({ config: backendConfig, fetch: fetchMock, document });
      const container = document.createElement("div");

      const result = await client.loadPackages(container);

      expect(result.status).toBe("error");
      expect(result.error).toBeTruthy();
      expect(container.textContent).toMatch(/gagal memuat paket/i);
    });

    it("tells the user the Package list is not configured when the backend URL is missing", async () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { packagesFromBackend: 1, backendBaseUrl: "" }, fetch: vi.fn(), document });
      const container = document.createElement("div");

      const result = await client.loadPackages(container);

      expect(result.status).toBe("error");
      expect(container.textContent).toMatch(/belum dikonfigurasi/i);
    });

    it("keeps the login seam usable after the Package backend fails", async () => {
      const { createVoucherClient } = loadModule();
      const fetchMock = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
      const client = createVoucherClient({ config: backendConfig, fetch: fetchMock, md5: loadRepoMd5(), document });
      const container = document.createElement("div");

      await client.loadPackages(container);

      const payload = client.login({ phoneNumber: "08123456789", pin: "123456" });
      expect(payload.username).toBe("08123456789");
    });
  });

  describe("hashPin / login", () => {
    it("hashes the PIN as CHAP(chapId + pin + chapChallenge), never sending the raw PIN", () => {
      const { createVoucherClient } = loadModule();
      const md5 = loadRepoMd5();
      const client = createVoucherClient({ md5 });

      const hash = client.hashPin("123456", "chap-id", "chap-challenge");

      expect(hash).toBe(expectedMd5("chap-id123456chap-challenge"));
      expect(hash).not.toContain("123456");
    });

    it("builds a Phone Number + hashed PIN payload and hands it to the submit hook", () => {
      const { createVoucherClient } = loadModule();
      const md5 = loadRepoMd5();
      const submit = vi.fn();
      const client = createVoucherClient({ md5 });

      const payload = client.login({ phoneNumber: "08123456789", pin: "123456", chapId: "id", chapChallenge: "ch", submit });

      expect(payload.username).toBe("08123456789");
      expect(payload.password).toBe(expectedMd5("id123456ch"));
      expect(submit).toHaveBeenCalledWith({ username: "08123456789", password: expectedMd5("id123456ch") });
    });

    it("requires a Phone Number and a PIN", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ md5: loadRepoMd5() });

      expect(() => client.login({ pin: "123456" })).toThrow(/phone number/i);
      expect(() => client.login({ phoneNumber: "0812" })).toThrow(/pin/i);
    });

    it("enforces the 6-digit PIN defined by ADR 0004", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ md5: loadRepoMd5() });

      expect(() => client.login({ phoneNumber: "0812", pin: "12345" })).toThrow(/6 digits/i);
      expect(() => client.login({ phoneNumber: "0812", pin: "abcdef" })).toThrow(/6 digits/i);
    });
  });

  describe("describeError", () => {
    it("maps an expired account (from the expiry variable) to an expiry message", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      expect(client.describeError("", "expired")).toMatch(/berakhir/i);
      expect(client.describeError("uptime limit reached", "")).toMatch(/berakhir/i);
    });

    it("maps a wrong PIN to a PIN-specific message", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      expect(client.describeError("invalid password", "")).toMatch(/PIN salah/i);
      expect(client.describeError("wrong password", "")).toMatch(/PIN salah/i);
    });

    it("falls back to a generic credential message", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      expect(client.describeError("some unrecognised hotspot error", "")).toMatch(/Periksa Phone Number dan PIN/i);
    });
  });
});
