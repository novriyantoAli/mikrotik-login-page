import { expect, test } from '@playwright/test';

import { configureDeployment } from '../smoke/helpers.mjs';
import {
  appOrigin,
  expectNoHorizontalScroll,
  loginPageOrigin,
  readMagicLink,
  stubOrigin,
  uniquePhoneNumber,
} from './helpers.mjs';

/**
 * #17's journey, over the real stack (ADR 0018 §5): Login Page → Package →
 * Magic Link → payment → **PIN** in Account Status → back to the Login Page.
 *
 * What is real: the app, the API, the worker, Postgres, the MySQL FreeRADIUS
 * tables, the SMS outbox, the callback signature and the state machine.
 * What is stubbed, on purpose: the vendor (a signed-callback stub) and the modem
 * (a device that does not exist). RouterOS and Tripay itself are out of scope and
 * stated as such in the ADR — a test that claimed otherwise would be lying.
 *
 * The viewport is 360×740, so the same run also proves the layout criterion on
 * every screen it touches.
 */
test.describe('perjalanan penuh', () => {
  test('@stub Login Page → Package → Magic Link → bayar → PIN di Account Status', async ({
    page,
  }) => {
    const phone = uniquePhoneNumber();
    const app = appOrigin();
    const loginPage = `${loginPageOrigin()}/login.html`;

    // 1. The Login Page reads the catalog from the real backend, configured the
    //    way a deployment does it: no page asset is edited (assets are frozen).
    await configureDeployment(page, {
      backendBaseUrl: `${app}/api`,
      appBaseUrl: `${app}/beli`,
    });
    await page.goto('/login.html');

    const packageCard = page.locator('.package-card').filter({ hasText: 'Paket Harian' });
    await expect(packageCard).toBeVisible();
    await expect(packageCard.locator('.package-card__buy')).toBeVisible();
    await expectNoHorizontalScroll(page, 'Login Page');

    // 2. The CTA carries the chosen Package and `return` into the app.
    await packageCard.locator('.package-card__buy').click();
    await expect(page).toHaveURL(/\/beli\?package=\d+/);

    const appUrl = new URL(page.url());
    const packageId = appUrl.searchParams.get('package');
    expect(packageId).not.toBeNull();
    expect(appUrl.searchParams.get('return')).toBe(loginPage);

    // 3. No session yet, so the app asks for the Phone Number first.
    await expect(page.getByText('Masuk dengan Phone Number')).toBeVisible();
    await expectNoHorizontalScroll(page, 'layar masuk');

    await page.locator('#phone-number').fill(phone.input);
    await page.getByRole('button', { name: 'Kirim tautan masuk' }).click();
    await expect(page.getByText('Periksa SMS Anda')).toBeVisible();

    // 4. The Magic Link exists only in the undelivered SMS body: the stored
    //    session is a hash and the token row never keeps the token (ADR 0011).
    const magicLink = await readMagicLink(phone.stored);

    // 5. Redeeming it sets the session cookie.
    const exchange = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/auth/magic-link/exchange') &&
        response.request().method() === 'POST',
    );
    await page.goto(magicLink);
    expect((await exchange).status()).toBe(200);

    // 6. Checkout: the real channel list, then a pending Transaction.
    await page.goto(`${app}/beli?package=${packageId}&return=${encodeURIComponent(loginPage)}`);
    await expect(page.getByText('Kanal pembayaran', { exact: true })).toBeVisible();
    await expectNoHorizontalScroll(page, 'layar checkout');

    await page.locator('label', { hasText: 'BRI Virtual Account' }).click();

    const checkout = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/transactions') && response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Lanjut ke pembayaran' }).click();

    const created = await (await checkout).json();
    expect(created.data.state).toBe('pending');
    expect(created.data.reference).toMatch(/\S/);

    await expect(page.getByText('Selesaikan pembayaran')).toBeVisible();
    await expect(page.getByText('Kode bayar')).toBeVisible();
    await expectNoHorizontalScroll(page, 'layar instruksi pembayaran');

    // 7. Pay on the vendor's page. The stub signs the callback with the same
    //    private key the backend verifies, so this is the production path.
    await page.goto(
      `${stubOrigin()}/bayar?reference=${encodeURIComponent(created.data.reference)}`,
    );
    await page.getByRole('button', { name: 'Bayar sekarang' }).click();
    await expect(page.getByTestId('callback-status')).toHaveText('200');

    // 8. `paid` is not the end: the worker still has to write the account, so
    //    the PIN only exists once the Transaction is provisioned (ADR 0016).
    await expect
      .poll(
        async () => {
          const response = await page.request.get(`${app}/api/transactions/${created.data.id}`);
          const body = await response.json();
          return body.data.state;
        },
        { timeout: 180_000, intervals: [1_000, 2_000, 5_000] },
      )
      .toBe('provisioned');

    // 9. The PIN a buyer actually needs, on the screen built for it.
    await page.goto(`${app}/akun`);
    await expect(page.getByText('PIN saat ini')).toBeVisible();
    await page.getByRole('button', { name: 'Tunjukkan PIN' }).click();

    const pin = (await page.getByTestId('account-pin').innerText()).trim();
    expect(pin).toMatch(/^\d{6}$/);
    await expectNoHorizontalScroll(page, 'Account Status');

    // 10. Back to the Login Page, where the buyer logs in with Phone Number + PIN.
    await page.goto(loginPage);
    await expect(page.locator('#username')).toBeVisible();
    await expect(page.locator('#btnLogin')).toBeEnabled();
  });
});
