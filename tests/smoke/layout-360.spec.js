import { test, expect } from "@playwright/test";
import {
  PACKAGE_FIXTURE,
  configureDeployment,
  stubPackageBackend,
} from "./helpers.mjs";

/**
 * The Login Page at 360px, which is the width #17 asks every screen to be usable
 * on (PRD point 29). The assertion is the acceptance criterion itself: the
 * document must never be wider than the viewport, so nothing hides behind a
 * horizontal scroll.
 *
 * It belongs here next to the other Login Page checks rather than in
 * `tests/app/layout.spec.mjs`, because the two pages are served by different
 * things: the app is built by SvelteKit, the Login Page is the frozen
 * `KOPINETGO-2024/` asset the router serves (ADR 0018 §5). The Package list is
 * stubbed through the deployment config, so the widest real state — packages
 * with names, prices and a "Beli" button — is what gets measured.
 */
test.use({ viewport: { width: 360, height: 740 } });

test("Login Page dengan daftar Package nyaman pada 360px", async ({ page }) => {
  await configureDeployment(page, {
    backendBaseUrl: "https://api.test",
    appBaseUrl: "https://app.test/beli",
  });
  await stubPackageBackend(page, [
    PACKAGE_FIXTURE,
    {
      id: 2,
      nama: "Paket Mingguan Hemat Keluarga",
      harga: "Rp 35.000",
      durasiActivePeriod: "7 Hari",
      batasKecepatan: "20M",
    },
  ]);

  await page.goto("/login.html");

  // Wait for the package list itself, not the skeleton: measuring a placeholder
  // would test the wrong thing.
  await expect(page.getByText(PACKAGE_FIXTURE.nama).first()).toBeVisible();
  await expect(page.getByRole("button", { name: /beli/i }).first()).toBeVisible();

  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));

  expect(
    overflow.scrollWidth,
    `/login.html lebih lebar dari viewport 360px (${overflow.scrollWidth} > ${overflow.clientWidth})`,
  ).toBeLessThanOrEqual(overflow.clientWidth);
});

test("Login Page tanpa backend tetap nyaman pada 360px", async ({ page }) => {
  await page.goto("/login.html");

  await expect(page.locator("#loginPage")).toBeVisible();
  await expect(page.locator("#btnLogin")).toBeVisible();

  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));

  expect(
    overflow.scrollWidth,
    `/login.html (tanpa Package) lebih lebar dari viewport 360px ` +
      `(${overflow.scrollWidth} > ${overflow.clientWidth})`,
  ).toBeLessThanOrEqual(overflow.clientWidth);
});
