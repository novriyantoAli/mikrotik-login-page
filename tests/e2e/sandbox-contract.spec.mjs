import { expect, test } from '@playwright/test';

import { configureDeployment } from '../smoke/helpers.mjs';
import { appOrigin, loginPageOrigin, readMagicLink, uniquePhoneNumber } from './helpers.mjs';

/**
 * The vendor contract, checked against Tripay's real sandbox.
 *
 * This is the one thing a stub cannot keep honest: whether the real API still
 * answers with the shape the adapter assumes (`/merchant/payment-channel`,
 * `/transaction/create`) and whether this merchant still has an active
 * closed-payment channel. It deliberately stops before paying — settling real
 * money, or waiting for a callback that a CI runner cannot receive, is not what
 * this check is for.
 *
 * Not part of the gate: run it when the vendor announces a change, or on a
 * schedule, against a stack whose TRIPAY_* are real:
 *
 *   E2E_SANDBOX=1 npx playwright test -c playwright.e2e.config.mjs --project=sandbox
 *
 * Reading the Magic Link needs database access to that stack; set E2E_PSQL_CMD
 * when it is not the compose one (see tests/e2e/helpers.mjs).
 */
test.describe('kontrak vendor', () => {
  test('@sandbox Tripay sandbox masih menjawab dengan bentuk yang diasumsikan adapter', async ({
    page,
  }) => {
    const phone = uniquePhoneNumber();
    const app = appOrigin();
    const loginPage = `${loginPageOrigin()}/login.html`;

    await configureDeployment(page, {
      backendBaseUrl: `${app}/api`,
      appBaseUrl: `${app}/beli`,
    });
    await page.goto('/login.html');

    const packageCard = page.locator('.package-card').first();
    await expect(packageCard).toBeVisible();
    await packageCard.locator('.package-card__buy').click();
    await expect(page).toHaveURL(/\/beli\?package=\d+/);

    const packageId = new URL(page.url()).searchParams.get('package');

    await expect(page.getByText('Masuk dengan Phone Number')).toBeVisible();
    await page.locator('#phone-number').fill(phone.input);
    await page.getByRole('button', { name: 'Kirim tautan masuk' }).click();
    await expect(page.getByText('Periksa SMS Anda')).toBeVisible();

    const magicLink = await readMagicLink(phone.stored);
    const exchange = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/auth/magic-link/exchange') &&
        response.request().method() === 'POST',
    );
    await page.goto(magicLink);
    expect((await exchange).status()).toBe(200);

    await page.goto(`${app}/beli?package=${packageId}&return=${encodeURIComponent(loginPage)}`);

    // The channel list is the vendor's answer, filtered to closed payments. An
    // empty list means the merchant lost its channels or the group vocabulary
    // changed — both are exactly what this check exists to catch.
    await expect(page.getByText('Kanal pembayaran', { exact: true })).toBeVisible();
    const channels = page.locator('input[name="channel"]');
    expect(await channels.count()).toBeGreaterThan(0);

    await channels.first().check();

    const checkout = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/transactions') && response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Lanjut ke pembayaran' }).click();
    const response = await checkout;

    expect(
      response.status(),
      'POST /api/transactions ditolak; adapter tidak cocok dengan jawaban vendor',
    ).toBe(200);

    const created = await response.json();
    expect(created.data.state).toBe('pending');

    // A real create answer must carry something the buyer can act on: a pay code
    // or a QR. Without one the adapter parsed a shape it did not understand.
    await expect(page.getByText('Selesaikan pembayaran')).toBeVisible();
    const hasPayCode = await page.getByText('Kode bayar').isVisible();
    const hasQR = await page.getByAltText('Kode QR pembayaran').isVisible();
    expect(hasPayCode || hasQR, 'tidak ada kode bayar maupun QR').toBe(true);

    // Deliberately not paid: settlement is covered by the @stub journey, which
    // can receive the callback.
    test.info().annotations.push({
      type: 'vendor',
      description: `reference ${created.data.reference} dibuat di sandbox, tidak dibayar`,
    });
  });
});
