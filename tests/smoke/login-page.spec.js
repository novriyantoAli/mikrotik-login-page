import { test, expect } from "@playwright/test";

const PACKAGE_FIXTURE = {
  id: 1,
  nama: "Paket Harian",
  harga: "Rp 5.000",
  durasiActivePeriod: "1 Hari",
  batasKecepatan: "10M",
};

/** Point the deployment config at stubbed backend/purchase URLs for this page load. */
async function configureDeployment(page, overrides) {
  await page.route("**/config/data/voucher.js*", async (route) => {
    const response = await route.fetch();
    let body = await response.text();
    if (overrides.backendBaseUrl !== undefined) {
      body = body.replace('backendBaseUrl: ""', `backendBaseUrl: "${overrides.backendBaseUrl}"`);
    }
    if (overrides.appBaseUrl !== undefined) {
      body = body.replace('appBaseUrl: ""', `appBaseUrl: "${overrides.appBaseUrl}"`);
    }
    await route.fulfill({ response, body });
  });
}

async function stubPackageBackend(page, packages) {
  await page.route("https://api.test/packages", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ data: packages }),
    });
  });
}

test.describe("Login Page smoke", () => {
  test("shows a clear Package error and keeps hotspot login usable when no backend is configured", async ({ page }) => {
    const voucherProblems = [];
    page.on("console", (message) => {
      if (message.type() === "error" && /voucher/i.test(message.text())) {
        voucherProblems.push(message.text());
      }
    });
    page.on("pageerror", (error) => {
      if (/voucher/i.test(String(error))) {
        voucherProblems.push(String(error));
      }
    });

    await page.goto("/login.html");

    // The existing Login Page still renders its login form.
    await expect(page).toHaveTitle(/KOPINETGO/i);
    await expect(page.locator("#loginPage")).toBeVisible();
    await expect(page.locator("#username")).toBeVisible();
    await expect(page.locator("#btnLogin")).toBeVisible();
    await expect(page.locator("#btnLogin")).toBeEnabled();

    // The voucher client is exposed as one public interface.
    const exposed = await page.evaluate(() => {
      const api = window.VoucherClient;
      return Boolean(
        api &&
          api.client &&
          typeof api.client.fetchPackages === "function" &&
          typeof api.client.loadPackages === "function" &&
          typeof api.client.renderPackages === "function" &&
          typeof api.client.buildPurchaseUrl === "function" &&
          typeof api.client.hashPin === "function" &&
          typeof api.client.login === "function",
      );
    });
    expect(exposed).toBe(true);

    // The Package section is mounted and, with no backend configured, degrades
    // to a clear message instead of blocking the login form.
    await expect(page.locator("#paketInternet")).toBeVisible();
    await expect(page.locator("#paketInternet")).toContainText(/belum dikonfigurasi/i);

    // Loading the section never leaves an uncaught voucher error behind.
    expect(voucherProblems).toEqual([]);
  });

  test("renders the Package list from the backend response", async ({ page }) => {
    await configureDeployment(page, { backendBaseUrl: "https://api.test" });
    await stubPackageBackend(page, [PACKAGE_FIXTURE]);

    await page.goto("/login.html");

    const packages = page.locator("#paketInternet");
    await expect(packages).toContainText("Paket Harian");
    await expect(packages).toContainText("Rp 5.000");
    await expect(packages).toContainText("1 Hari");
    await expect(packages).toContainText("10M");

    await expect(page.locator("#btnLogin")).toBeEnabled();
  });

  test("sends the chosen Package and the Login Page return target to the purchase app on Beli", async ({ page }) => {
    await configureDeployment(page, { backendBaseUrl: "https://api.test", appBaseUrl: "https://app.test/beli" });
    await stubPackageBackend(page, [PACKAGE_FIXTURE]);
    await page.route("https://app.test/**", async (route) => {
      await route.fulfill({ status: 200, contentType: "text/html", body: "<html><body>purchase app</body></html>" });
    });

    // The Login Page keeps its hotspot context in the query string.
    await page.goto("/login.html?dst=http%3A%2F%2Fexample.com");
    const loginUrl = page.url();

    await page.locator("#paketInternet").getByRole("button", { name: "Beli", exact: true }).click();

    await page.waitForURL(/app\.test\/beli/);
    const purchaseUrl = new URL(page.url());
    expect(purchaseUrl.searchParams.get("package")).toBe("1");
    expect(purchaseUrl.searchParams.get("return")).toBe(loginUrl);
    expect(purchaseUrl.searchParams.get("return")).toContain("dst=");
  });
});
