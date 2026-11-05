import { expect, test } from '@playwright/test';

import { appOrigin } from './helpers.mjs';

/**
 * #34's journey: the Admin Panel as it is actually deployed — its own image,
 * behind the one proxy, at `/admin` on the same origin as the API (ADR 0028).
 *
 * What it proves that no unit test can: the shell is served by the panel's own
 * container with its own cache and content policy, its sign-in survives the
 * proxy hop with a cookie scoped to `/api/admin` (ADR 0024 §2), and a price
 * written through the panel is the price the Login Page reads from the public
 * catalog. What it deliberately does not prove: the panel's field rules, which
 * are the backend's (and the zod schema's) job.
 *
 * A buyer's session and an admin's must be able to coexist, so the spec never
 * touches the buyer's cookies.
 */

// The e2e stack seeds the first owner from ADMIN_EMAIL/ADMIN_PASSWORD
// (docker-compose.e2e.yml), so the spec signs in with the same pair: a deployment
// that renamed them fails here rather than in a support call. The fallbacks are
// the e2e compose defaults, which is what `cp .env.example .env` reproduces (both
// keys ship empty, and compose treats an empty value as unset).
const adminEmail = process.env.ADMIN_EMAIL || 'owner@e2e.test';
const adminPassword = process.env.ADMIN_PASSWORD || 'kata-sandi-e2e-yang-panjang';

test.describe('panel admin', () => {
  test('@panel /admin menyajikan panel, dan harga yang ditulis sampai ke katalog publik', async ({
    page,
  }) => {
    const app = appOrigin();

    // A policy that blocks SvelteKit's inline bootstrap leaves a blank screen and
    // no failed request, so surface the cause instead of "element not found".
    const cspViolations = [];
    page.on('console', (message) => {
      if (message.type() === 'error' && /Content Security Policy/i.test(message.text())) {
        cspViolations.push(message.text());
      }
    });

    // 1. The shell, served by the panel's own image under /admin. `/admin`
    //    redirects to `/admin/`; the final document is the panel's.
    const shell = await page.goto(`${app}/admin`);
    expect(new URL(page.url()).pathname).toBe('/admin/');
    expect(shell?.status()).toBe(200);
    expect(shell?.headers()['cache-control']).toContain('no-store');

    await expect(page.getByRole('heading', { name: 'Panel Admin', exact: true })).toBeVisible();
    await expect(page.getByText('Masuk Panel Admin')).toBeVisible();
    expect(cspViolations, 'situs ini memblokir bootstrap SPA panel').toEqual([]);

    // 2. The panel's policy is its own, not the purchase app's: no vendor QR
    //    image to allow, so no `https:` source.
    const panelPolicy = await page.evaluate(
      () =>
        document
          .querySelector('meta[http-equiv="content-security-policy"]')
          ?.getAttribute('content') ?? '',
    );
    expect(panelPolicy).toContain('default-src');
    expect(panelPolicy).not.toMatch(/img-src[^;]*https:/);

    // The purchase app is untouched by the panel's arrival, and its own policy is
    // still the looser one — the two documents differ on purpose (ADR 0028 §4).
    const appRoot = await page.request.get(`${app}/`);
    expect(appRoot.status()).toBe(200);
    expect(await appRoot.text()).toMatch(/img-src[^;]*https:/);

    // 3. Sign in. The cookie is the session, and it is scoped to the API path,
    //    which is what lets the panel's own path change without touching auth.
    await page.locator('#admin-email').fill(adminEmail);
    await page.locator('#admin-password').fill(adminPassword);
    await page.getByRole('button', { name: 'Masuk' }).click();
    await expect(page.getByText('Yang bisa Anda lakukan')).toBeVisible();

    const session = (await page.context().cookies(`${app}/api/admin`)).find(
      (cookie) => cookie.name === 'hotspot_admin',
    );
    expect(session, 'panel tidak menerima cookie sesi').toBeTruthy();
    expect(session?.path).toBe('/api/admin');
    expect(session?.httpOnly).toBe(true);

    // 4. Read one Package through the panel's own API, so the edit below is based
    //    on the version the form would have read.
    const adminCatalog = async () =>
      (await (await page.request.get(`${app}/api/admin/packages`)).json()).data;
    const publicCatalog = async () =>
      (await (await page.request.get(`${app}/api/packages`)).json()).data;

    // Paket Mingguan, not Paket Harian: the journey pins the cheapest Package to
    // the QRIS-only channel window (ADR 0021), so its price must stay put.
    const original = (await adminCatalog()).find((pkg) => pkg.code === 'mingguan');
    expect(original, 'seed katalog tidak memuat Paket Mingguan').toBeTruthy();

    const raisedPrice = original.price_idr + 1000;
    const expectedHarga = `Rp ${raisedPrice.toLocaleString('id-ID')}`;

    try {
      // 5. Change the price on the panel's catalog screen.
      await page.goto(`${app}/admin/paket`);
      await expect(page.getByRole('heading', { name: 'Katalog Package' })).toBeVisible();

      await page
        .getByRole('row')
        .filter({ hasText: original.code })
        .getByRole('button', { name: 'Ubah' })
        .click();
      await page.locator('#package-price').fill(String(raisedPrice));
      await page.getByRole('button', { name: 'Simpan perubahan' }).click();

      // 6. The write shows up on the panel's own list, and then in the catalog
      //    the Login Page reads — the whole point of the panel: a price change
      //    without a deploy and without SQL.
      await expect(
        page.getByRole('row').filter({ hasText: original.code }),
      ).toContainText(raisedPrice.toLocaleString('id-ID'));

      await expect
        .poll(
          async () => (await publicCatalog()).find((pkg) => pkg.nama === original.name)?.harga,
          { timeout: 30_000 },
        )
        .toBe(expectedHarga);
    } finally {
      // 7. Put the catalog back. The journey that runs next asserts prices, and a
      //    failed assertion above must not leave the stack in a different catalog
      //    than a fresh deployment has. Written through the API for the same
      //    reason: this is cleanup, not another journey.
      const current = (await adminCatalog()).find((pkg) => pkg.id === original.id);
      const restored = await page.request.put(`${app}/api/admin/packages/${original.id}`, {
        data: {
          code: current.code,
          name: current.name,
          price_idr: original.price_idr,
          duration_minutes: current.duration_minutes,
          rate_limit: current.rate_limit,
          version: current.version,
        },
      });
      expect(restored.status(), 'katalog tidak bisa dikembalikan ke harga semula').toBe(200);
    }
  });
});
