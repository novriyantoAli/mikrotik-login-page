import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { expect, test } from '@playwright/test';

/**
 * Every screen at 360×740, which is the narrowest width the purchase app has to
 * be usable on (PRD point 29/30, #17). The assertion is the acceptance criterion
 * itself: the document must never be wider than the viewport, so nothing can
 * hide behind a horizontal scroll.
 *
 * The API is intercepted with the shared contract fixtures, so this spec needs
 * no backend and no Docker — the full journey over the real stack is a separate
 * spec (ADR 0018 §5).
 */
const contractsDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../contracts');

const contract = (name) => JSON.parse(readFileSync(path.join(contractsDir, name), 'utf8'));

const unauthorized = {
  status: 401,
  body: {
    error: {
      code: 'unauthorized',
      message: 'Sesi tidak ditemukan atau sudah berakhir. Minta tautan masuk baru.',
    },
  },
};

const signedIn = { body: contract('session.response.json') };

/**
 * stubApi answers every `/api/**` request from the fixtures. Anything the list
 * does not name fails loudly, so a screen that grows a new request cannot pass
 * silently on an empty body.
 */
async function stubApi(page, responses) {
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url());
    const match = responses.find((response) => response.match(url));

    if (!match) {
      throw new Error(`unexpected API call in a layout test: ${url.pathname}${url.search}`);
    }

    await route.fulfill({
      status: match.status ?? 200,
      contentType: 'application/json',
      body: JSON.stringify(match.body ?? {}),
    });
  });
}

const get = (pathname, body, status = 200) => ({
  match: (url) => url.pathname === pathname,
  body,
  status,
});

const screens = [
  {
    name: 'status sistem',
    path: '/',
    readyText: 'Status Sistem',
    responses: [get('/api/health', contract('health.response.json'))],
  },
  {
    name: 'masuk (belum punya sesi)',
    path: '/masuk',
    readyText: 'Masuk dengan Phone Number',
    responses: [get('/api/auth/session', unauthorized.body, unauthorized.status)],
  },
  {
    name: 'masuk dengan tautan tidak lengkap',
    path: '/masuk/tautan',
    readyText: 'Tautan tidak lengkap',
    responses: [],
  },
  {
    name: 'beli (pilih kanal)',
    path: '/beli?package=1',
    // A signed-in buyer sees the widest version of this screen: Package summary,
    // channel list and the payment button.
    readyText: 'Lanjut ke pembayaran',
    responses: [
      get('/api/auth/session', signedIn.body),
      get('/api/packages', contract('packages.response.json')),
      get('/api/transactions/channels', contract('channels.response.json')),
      get('/api/account', contract('account.response.json')),
      get('/api/account/extend-preview', contract('account.extend-preview.response.json')),
    ],
  },
  {
    name: 'Account Status',
    path: '/akun',
    readyText: 'Riwayat Transaction',
    responses: [
      get('/api/auth/session', signedIn.body),
      get('/api/account', contract('account.response.json')),
      get('/api/transactions', contract('transactions.response.json')),
    ],
  },
  {
    name: 'detail Transaction',
    path: '/transaksi/12',
    readyText: 'Kembali ke Account Status',
    responses: [
      get('/api/auth/session', signedIn.body),
      get('/api/transactions/12', contract('transaction.provisioned.response.json')),
    ],
  },
];

for (const screen of screens) {
  test(`${screen.name} nyaman pada 360px`, async ({ page }) => {
    await stubApi(page, screen.responses);
    await page.goto(screen.path);

    // Wait for the screen to finish its first render: measuring a skeleton would
    // test the placeholder, not the page.
    await expect(page.getByText(screen.readyText).first()).toBeVisible();

    const overflow = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
    }));

    expect(
      overflow.scrollWidth,
      `${screen.path} lebih lebar dari viewport 360px (${overflow.scrollWidth} > ${overflow.clientWidth})`,
    ).toBeLessThanOrEqual(overflow.clientWidth + 1);
  });
}
