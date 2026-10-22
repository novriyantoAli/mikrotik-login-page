import { test, expect } from "@playwright/test";
import { PACKAGE_FIXTURE, configureDeployment, stubPackageBackend, stubActivePeriodExpiry, servePage } from "./helpers.mjs";

const HOUR = 3600000;
const DAY = 24 * HOUR;

async function openStatusPage(page, { expiry }) {
  await configureDeployment(page, { backendBaseUrl: "https://api.test" });
  await stubPackageBackend(page, [PACKAGE_FIXTURE]);
  if (expiry !== undefined) {
    await stubActivePeriodExpiry(page, expiry);
  }
  await servePage(page, "**/status.html*", { "$(username)": "08123456789" });
  await page.goto("/status.html");
}

test.describe("Status page smoke", () => {
  test("shows the remaining Active Period and an Extend action while active", async ({ page }) => {
    await openStatusPage(page, { expiry: new Date(Date.now() + 3 * DAY).toISOString() });

    const activePeriod = page.locator("#activePeriod");
    await expect(activePeriod).toContainText(/sisa active period/i);
    await expect(activePeriod).toContainText(/extend/i);
    await expect(activePeriod.locator("a")).toHaveAttribute("href", "#paketInternet");
    await expect(page.locator("#btnLogout")).toBeVisible();
  });

  test("warns when the Active Period is almost over", async ({ page }) => {
    await openStatusPage(page, { expiry: new Date(Date.now() + 2 * HOUR).toISOString() });

    await expect(page.locator("#activePeriod")).toContainText(/hampir habis/i);
    await expect(page.locator("#activePeriod")).toContainText(/extend/i);
  });

  test("directs an expired Account back to the Package list to buy again", async ({ page }) => {
    await openStatusPage(page, { expiry: new Date(Date.now() - HOUR).toISOString() });

    const activePeriod = page.locator("#activePeriod");
    await expect(activePeriod).toContainText(/berakhir/i);
    await expect(activePeriod).toContainText(/beli paket baru/i);
    await expect(activePeriod.locator("a")).toHaveAttribute("href", "#paketInternet");
  });

  test("keeps logout working when the Account backend fails", async ({ page }) => {
    await configureDeployment(page, { backendBaseUrl: "https://api.test" });
    await page.route("https://api.test/active-period*", (route) => route.abort());
    await servePage(page, "**/status.html*", { "$(username)": "08123456789" });
    await page.goto("/status.html");

    await expect(page.locator("#activePeriod")).toContainText(/tidak dapat dimuat/i);
    await expect(page.locator("#btnLogout")).toBeVisible();
    await expect(page.locator("#btnLogout")).toBeEnabled();
  });
});
