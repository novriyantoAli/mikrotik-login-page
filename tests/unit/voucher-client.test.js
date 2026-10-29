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
    delete globalThis.cfgLogin;
    delete globalThis.cfgIP;
  });

  it("exposes a single public interface", () => {
    const VoucherClient = loadModule();
    expect(typeof VoucherClient.createVoucherClient).toBe("function");
    expect(typeof VoucherClient.resolveConfig).toBe("function");
    expect(typeof VoucherClient.client.fetchPackages).toBe("function");
    expect(typeof VoucherClient.client.loadPackages).toBe("function");
    expect(typeof VoucherClient.client.fetchActivePeriod).toBe("function");
    expect(typeof VoucherClient.client.activePeriodStatus).toBe("function");
    expect(typeof VoucherClient.client.renderActivePeriod).toBe("function");
    expect(typeof VoucherClient.client.renderPackages).toBe("function");
    expect(typeof VoucherClient.client.buildPurchaseUrl).toBe("function");
    expect(typeof VoucherClient.client.hashPin).toBe("function");
    expect(typeof VoucherClient.client.login).toBe("function");
    expect(typeof VoucherClient.client.loginModes).toBe("function");
    expect(typeof VoucherClient.client.loginModeDescriptor).toBe("function");
    expect(typeof VoucherClient.client.renderLoginModes).toBe("function");
    expect(typeof VoucherClient.client.loginCredentials).toBe("function");
    expect(typeof VoucherClient.resolveLoginModes).toBe("function");
    expect(typeof VoucherClient.client.describeError).toBe("function");
    expect(typeof VoucherClient.client.describeLoginError).toBe("function");
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

      // The theme block in style.css hangs off these hooks: name and price on one
      // line, the Package attributes as pills, then the Buy pill.
      expect(cards[0].querySelector(".package-card__head .package-card__name")).not.toBeNull();
      expect(cards[0].querySelector(".package-card__head .package-card__price")).not.toBeNull();
      const meta = cards[0].querySelectorAll(".package-card__meta .package-card__period, .package-card__meta .package-card__speed");
      expect(meta).toHaveLength(2);

      const buyButtons = container.querySelectorAll("button");
      expect(buyButtons).toHaveLength(2);
      expect(buyButtons[0].textContent).toBe("Beli");
      expect(buyButtons[0].classList.contains("package-card__buy")).toBe(true);
    });

    it("titles the section with the deployment's own Package copy", () => {
      globalThis.cfgIP = { label: { title: "Paket Internet", sub_title: "Pilih yang paling cocok" } };
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { appBaseUrl: "https://app.example/beli" }, document });
      const container = document.createElement("div");

      client.renderPackages(container, packages);

      expect(container.querySelector(".package-list__title").textContent).toBe("Paket Internet");
      expect(container.querySelector(".package-list__subtitle").textContent).toBe("Pilih yang paling cocok");
    });

    it("keeps its own heading when the deployment has no Package labels", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ config: { appBaseUrl: "https://app.example/beli" }, document });
      const container = document.createElement("div");

      client.renderPackages(container, packages);

      expect(container.querySelector(".package-list__title").textContent).toMatch(/paket internet/i);
      expect(container.querySelector(".package-list__subtitle")).not.toBeNull();
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

  describe("active period status", () => {
    const HOUR = 3600000;
    const DAY = 24 * HOUR;
    const NOW = Date.UTC(2026, 0, 10, 12, 0, 0);

    it("reports an active Account with the remaining Active Period and an Extend action", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      const status = client.activePeriodStatus(new Date(NOW + 2 * DAY + 3 * HOUR), { now: NOW, warningMs: DAY });

      expect(status.state).toBe("active");
      expect(status.remainingText).toBe("2 hari 3 jam");
      expect(status.actionLabel).toMatch(/extend/i);
    });

    it("flags an expiring Active Period as almost over", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      const status = client.activePeriodStatus(new Date(NOW + 2 * HOUR), { now: NOW, warningMs: DAY });

      expect(status.state).toBe("expiring");
      expect(status.message).toMatch(/hampir habis/i);
    });

    it("reports an ended Active Period with a buy-again action", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      const status = client.activePeriodStatus(new Date(NOW - HOUR), { now: NOW, warningMs: DAY });

      expect(status.state).toBe("expired");
      expect(status.message).toMatch(/berakhir/i);
      expect(status.actionLabel).toMatch(/beli/i);
    });

    it("returns null when there is no usable expiry", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      expect(client.activePeriodStatus(null)).toBeNull();
      expect(client.activePeriodStatus("not-a-date")).toBeNull();
    });

    it("formats the remaining Active Period in days, hours and minutes", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      expect(client.formatActivePeriod(2 * DAY + 3 * HOUR)).toBe("2 hari 3 jam");
      expect(client.formatActivePeriod(2 * DAY + 30 * 60000)).toBe("2 hari 30 menit");
      expect(client.formatActivePeriod(45 * 60000)).toBe("45 menit");
    });

    it("accepts an epoch-seconds expiry (the FreeRADIUS Expiration convention)", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      const status = client.activePeriodStatus((NOW + 2 * DAY) / 1000, { now: NOW, warningMs: DAY });

      expect(status.state).toBe("active");
      expect(status.remainingText).toBe("2 hari");
    });
  });

  describe("renderActivePeriod", () => {
    it("shows the remaining Active Period, an indication and an Extend action", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ document });
      const container = document.createElement("div");
      const status = client.activePeriodStatus(new Date(Date.now() + 3 * 86400000), { warningMs: 86400000 });

      client.renderActivePeriod(container, status);

      expect(container.textContent).toMatch(/sisa active period/i);
      expect(container.textContent).toMatch(/extend/i);
      expect(container.querySelector("a").getAttribute("href")).toBe("#paketInternet");
    });

    it("directs an expired Account back to the Package list to buy again", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ document });
      const container = document.createElement("div");
      const status = client.activePeriodStatus(new Date(Date.now() - 1000), { warningMs: 86400000 });

      client.renderActivePeriod(container, status, { packagesTarget: "paketSaya" });

      expect(container.textContent).toMatch(/beli paket baru/i);
      expect(container.querySelector("a").getAttribute("href")).toBe("#paketSaya");
    });
  });

  describe("fetchActivePeriod", () => {
    it("fetches the end of the Active Period from the configured backend endpoint", async () => {
      const { createVoucherClient } = loadModule();
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ data: { expiry: "2026-01-10T12:00:00Z" } }) });
      const client = createVoucherClient({ config: { backendBaseUrl: "https://api.example", activePeriodPath: "/active-period" }, fetch: fetchMock });

      const account = await client.fetchActivePeriod("08123456789");

      expect(fetchMock.mock.calls[0][0]).toBe("https://api.example/active-period?phone=08123456789");
      expect(account.expiry).toBe("2026-01-10T12:00:00Z");
    });

    it("rejects with a coded error when the Active Period backend is unreachable", async () => {
      const { createVoucherClient } = loadModule();
      const fetchMock = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
      const client = createVoucherClient({ config: { backendBaseUrl: "https://api.example" }, fetch: fetchMock });

      await expect(client.fetchActivePeriod("0812")).rejects.toMatchObject({ code: "network_error" });
    });
  });

  describe("describeLoginError", () => {
    it("explains a missing Phone Number, a missing PIN, and a malformed PIN", () => {
      const { createVoucherClient, VoucherError } = loadModule();
      const client = createVoucherClient();

      expect(client.describeLoginError(new VoucherError("missing_phone_number", "x"))).toMatch(/phone number/i);
      expect(client.describeLoginError(new VoucherError("missing_pin", "x"))).toMatch(/pin wajib/i);
      expect(client.describeLoginError(new VoucherError("invalid_pin", "x"))).toMatch(/6 digit/i);
    });

    it("falls back to the generic credential message for an unknown failure", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      expect(client.describeLoginError(new Error("boom"))).toMatch(/periksa phone number dan pin/i);
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

  describe("login modes", () => {
    /** A detached stand-in for the Login Page form slots. */
    function buildLoginForm(document) {
      const title = document.createElement("p");
      const primaryGroup = document.createElement("div");
      const primaryInput = document.createElement("input");
      const primaryReveal = document.createElement("div");
      const secondaryGroup = document.createElement("div");
      const secondaryInput = document.createElement("input");
      const secondaryReveal = document.createElement("div");
      return {
        container: document.createElement("div"),
        title,
        primaryGroup,
        primaryInput,
        primaryReveal,
        secondaryGroup,
        secondaryInput,
        secondaryReveal,
        fields: {
          title,
          primary: { group: primaryGroup, input: primaryInput, reveal: primaryReveal },
          secondary: { group: secondaryGroup, input: secondaryInput, reveal: secondaryReveal },
        },
      };
    }

    it("enables only Phone Number + PIN by default", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      expect(client.loginModes().map((mode) => mode.id)).toEqual(["phone"]);
    });

    it("reads the enabled modes and their labels from the cfgLogin toggles", () => {
      globalThis.cfgLogin = { modes: { phone: { status: 1 }, user: { status: 1, label: "Pengguna" } } };
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      const modes = client.loginModes();
      expect(modes.map((mode) => mode.id)).toEqual(["phone", "user"]);
      expect(modes[0].label).toBe("Nomor HP");
      expect(modes[1].label).toBe("Pengguna");
    });

    it("starts in the mode the deployment names, even when it is not the first", () => {
      // A voucher hotspot lands the buyer on Kode Akses; the tab order stays
      // canonical, so the default is chosen explicitly (cfgLogin.defaultMode).
      globalThis.cfgLogin = {
        defaultMode: "accessCode",
        modes: { phone: { status: 1 }, accessCode: { status: 1 }, user: { status: 1 } },
      };
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ document });
      const form = buildLoginForm(document);
      form.title.textContent = "Masuk";

      expect(client.loginModes().map((mode) => mode.id)).toEqual(["phone", "accessCode", "user"]);
      expect(client.defaultLoginMode()).toBe("accessCode");

      const control = client.renderLoginModes(form.container, { fields: form.fields });
      expect(control.mode).toBe("accessCode");
      expect(form.title.textContent).toBe("Login");
      expect(form.primaryInput.getAttribute("placeholder")).toBe("Kode Akses");
      expect(form.secondaryGroup.classList.contains("hidden")).toBe(true);

      const tabs = form.container.querySelectorAll(".login-modes__tab");
      expect(tabs[0].getAttribute("aria-selected")).toBe("false");
      expect(tabs[1].getAttribute("aria-selected")).toBe("true");
    });

    it("ignores a default mode that is disabled or unknown", () => {
      globalThis.cfgLogin = {
        defaultMode: "accessCode",
        modes: { phone: { status: 1 }, accessCode: { status: 0 }, user: { status: 1 } },
      };
      const { createVoucherClient } = loadModule();
      expect(createVoucherClient().defaultLoginMode()).toBe("phone");

      globalThis.cfgLogin = { defaultMode: "magic", modes: { phone: { status: 0 }, user: { status: 1 } } };
      expect(createVoucherClient().defaultLoginMode()).toBe("user");

      globalThis.cfgLogin = { defaultMode: "accessCode", modes: { phone: { status: 0 }, accessCode: { status: 0 }, user: { status: 0 } } };
      expect(createVoucherClient().defaultLoginMode()).toBe("phone");
    });

    it("takes the default mode from explicit config as well as cfgLogin", () => {
      const { createVoucherClient, resolveLoginConfig } = loadModule();

      expect(resolveLoginConfig({ defaultMode: "user", modes: { user: { status: 1 } } }).defaultMode).toBe("user");

      const client = createVoucherClient({
        login: { defaultMode: "user", modes: { phone: { status: 1 }, user: { status: 1 } } },
      });
      expect(client.defaultLoginMode()).toBe("user");
      expect(client.loginModeDescriptor().id).toBe("user");
    });

    it("falls back to the default mode when every toggle is off", () => {
      const { resolveLoginModes } = loadModule();
      const modes = resolveLoginModes({ modes: { phone: { status: 0 }, accessCode: { status: 0 }, user: { status: 0 } } });

      expect(modes.map((mode) => mode.id)).toEqual(["phone"]);
    });

    it("rejects a mode the seam does not know", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ md5: loadRepoMd5() });

      expect(() => client.loginModeDescriptor("magic")).toThrow(/unknown login mode/i);
      expect(() => client.login({ mode: "magic", phoneNumber: "0812", pin: "123456" })).toThrow(/unknown login mode/i);
      expect(() => client.describeError("invalid password", "", "magic")).toThrow(/unknown login mode/i);
    });

    it("renders no tab and applies the descriptor when only one mode is enabled", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ document });
      const form = buildLoginForm(document);
      form.title.textContent = "Masuk";

      const control = client.renderLoginModes(form.container, { fields: form.fields });

      expect(control.mode).toBe("phone");
      expect(control.modes).toEqual(["phone"]);
      expect(form.container.classList.contains("hidden")).toBe(true);
      expect(form.container.querySelectorAll(".login-modes__tab")).toHaveLength(0);

      // With a single mode the page config keeps owning the title.
      expect(form.title.textContent).toBe("Masuk");
      expect(form.primaryInput.getAttribute("placeholder")).toBe("Phone Number");
      expect(form.secondaryInput.getAttribute("placeholder")).toBe("PIN");
      expect(form.secondaryInput.getAttribute("type")).toBe("password");
      expect(form.secondaryGroup.classList.contains("hidden")).toBe(false);
      expect(form.primaryReveal.style.display).toBe("none");
    });

    it("shows a segmented control once more than one mode is enabled and switches the field shape", () => {
      globalThis.cfgLogin = { modes: { phone: { status: 1 }, user: { status: 1 } } };
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ document });
      const form = buildLoginForm(document);
      form.title.textContent = "Masuk";
      const onChange = vi.fn();

      client.renderLoginModes(form.container, { fields: form.fields, onChange });

      expect(form.container.classList.contains("hidden")).toBe(false);
      expect(form.title.textContent).toBe("Login");
      const tabs = form.container.querySelectorAll(".login-modes__tab");
      expect(Array.from(tabs).map((tab) => tab.textContent)).toEqual(["Nomor HP", "User"]);
      expect(tabs[0].getAttribute("aria-selected")).toBe("true");

      tabs[1].click();

      expect(onChange).toHaveBeenCalledWith("user");
      expect(tabs[1].getAttribute("aria-selected")).toBe("true");
      expect(tabs[0].getAttribute("aria-selected")).toBe("false");
      expect(form.primaryInput.getAttribute("placeholder")).toBe("Username");
      expect(form.secondaryInput.getAttribute("placeholder")).toBe("Password");
      expect(form.secondaryReveal.style.display).toBe("");
    });

    it("renders a single field for the Access Code mode", () => {
      globalThis.cfgLogin = { modes: { phone: { status: 0 }, accessCode: { status: 1 } } };
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ document });
      const form = buildLoginForm(document);

      const control = client.renderLoginModes(form.container, { fields: form.fields });

      expect(control.mode).toBe("accessCode");
      expect(form.primaryInput.getAttribute("placeholder")).toBe("Kode Akses");
      expect(form.secondaryGroup.classList.contains("hidden")).toBe(true);
    });

    it("renders both fields for a User-only deployment", () => {
      globalThis.cfgLogin = { modes: { phone: { status: 0 }, user: { status: 1 } } };
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ document });
      const form = buildLoginForm(document);
      form.title.textContent = "Login";

      const control = client.renderLoginModes(form.container, { fields: form.fields });

      expect(control.mode).toBe("user");
      expect(control.modes).toEqual(["user"]);
      expect(form.container.classList.contains("hidden")).toBe(true);
      expect(form.title.textContent).toBe("Login");
      expect(form.primaryInput.getAttribute("placeholder")).toBe("Username");
      expect(form.secondaryInput.getAttribute("placeholder")).toBe("Password");
      expect(form.secondaryGroup.classList.contains("hidden")).toBe(false);
      expect(form.secondaryInput.getAttribute("type")).toBe("password");
    });

    it("maps the form slots onto each mode's credential keys", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      expect(client.loginCredentials("phone", { primary: "0812", secondary: "123456" })).toEqual({ phoneNumber: "0812", pin: "123456" });
      expect(client.loginCredentials("accessCode", { primary: "ABC123", secondary: "" })).toEqual({ accessCode: "ABC123" });
      expect(client.loginCredentials("user", { primary: "budi", secondary: "rahasia" })).toEqual({ username: "budi", password: "rahasia" });
    });

    it("submits an Access Code as username and password, hashed with CHAP, without the 6-digit rule", () => {
      const { createVoucherClient } = loadModule();
      const submit = vi.fn();
      const client = createVoucherClient({ md5: loadRepoMd5() });

      const payload = client.login({ mode: "accessCode", accessCode: "ABC", chapId: "id", chapChallenge: "ch", submit });

      expect(payload.username).toBe("ABC");
      expect(payload.password).toBe(expectedMd5("idABCch"));
      expect(submit).toHaveBeenCalledWith(payload);
      expect(() => client.login({ mode: "accessCode", accessCode: "12" })).not.toThrow();
    });

    it("submits a User with the username as typed and a separately hashed password of any length", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ md5: loadRepoMd5() });

      const payload = client.login({ mode: "user", username: "budi", password: "rahasia", chapId: "id", chapChallenge: "ch" });

      expect(payload.username).toBe("budi");
      expect(payload.password).toBe(expectedMd5("idrahasiach"));
      expect(payload.password).not.toBe(payload.username);

      // The username is sent exactly as typed and the password has no 6-digit
      // PIN rule, unlike Phone Number + PIN.
      const short = client.login({ mode: "user", username: "Budi Santoso", password: "abc", chapId: "id", chapChallenge: "ch" });

      expect(short.username).toBe("Budi Santoso");
      expect(short.password).toBe(expectedMd5("idabcch"));
    });

    it("hashes a User password even when it is typed the same as the username", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ md5: loadRepoMd5() });

      const payload = client.login({ mode: "user", username: "budi", password: "budi", chapId: "id", chapChallenge: "ch" });

      expect(payload.username).toBe("budi");
      expect(payload.password).toBe(expectedMd5("idbudich"));
      expect(payload.password).not.toBe("budi");
    });

    it("requires the credentials of the active mode", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient({ md5: loadRepoMd5() });

      expect(() => client.login({ mode: "accessCode", accessCode: "" })).toThrow(/access code is required/i);
      expect(() => client.login({ mode: "user", username: "budi" })).toThrow(/password is required/i);
      expect(() => client.login({ mode: "user", password: "rahasia" })).toThrow(/username is required/i);
    });

    it("follows the mode for credential validation messages", () => {
      const { createVoucherClient, VoucherError } = loadModule();
      const client = createVoucherClient();

      expect(client.describeLoginError(new VoucherError("missing_access_code", "x"), "accessCode")).toMatch(/kode akses wajib/i);
      expect(client.describeLoginError(new VoucherError("missing_username", "x"), "user")).toMatch(/username wajib/i);
      expect(client.describeLoginError(new VoucherError("missing_password", "x"), "user")).toMatch(/password wajib/i);
      expect(client.describeLoginError(new Error("boom"), "user")).toMatch(/periksa username dan password/i);
    });

    it("follows the mode for a wrong credential but keeps one expired message", () => {
      const { createVoucherClient } = loadModule();
      const client = createVoucherClient();

      expect(client.describeError("invalid password", "", "accessCode")).toBe("Kode Akses salah.");
      expect(client.describeError("invalid password", "", "user")).toMatch(/username atau password salah/i);
      expect(client.describeError("", "expired", "accessCode")).toMatch(/berakhir/i);
      expect(client.describeError("", "expired", "user")).toMatch(/berakhir/i);
    });
  });
});
