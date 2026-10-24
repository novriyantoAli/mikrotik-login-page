import { fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import AccountStatusFixture from '../support/AccountStatusFixture.svelte';
import { restoreApi, stubApiRoutes, type StubRoute, type StubbedResponse } from '../support/api';
import { loadContract } from '../support/contracts';

const session = { data: { phone_number: '6281234567890', expires_at: '2026-11-22T08:00:00Z' } };

function unauthorized() {
  return {
    status: 401,
    data: {
      error: {
        code: 'unauthorized',
        message: 'Sesi tidak ditemukan atau sudah berakhir. Minta tautan masuk baru.',
      },
    },
  };
}

function accountSummary(overrides: Record<string, unknown> = {}) {
  // The adapter returns the whole response body, envelope included.
  return {
    data: {
      data: {
        phone_number: '6281234567890',
        active_period: {
          expires_at: new Date(Date.now() + 3 * 24 * 60 * 60 * 1000).toISOString(),
          found: true,
          state: 'active',
        },
        package: { id: 1, name: 'Paket Harian', rate_limit: '5M/5M' },
        pin: '123456',
        ...overrides,
      },
    },
  };
}

function emptyHistory() {
  return { data: { data: [], total_count: 0, page: 1, page_size: 5 } };
}

function stubsFor(
  summary: () => StubbedResponse,
  history: () => StubbedResponse = emptyHistory,
): StubRoute[] {
  return [
    { method: 'get', path: '/auth/session', respond: () => ({ data: session }) },
    { method: 'get', path: '/account', respond: summary },
    { method: 'get', path: '/transactions', respond: history },
    {
      method: 'get',
      path: '/packages',
      respond: () => ({ data: loadContract('packages.response.json') }),
    },
  ];
}

describe('AccountStatus', () => {
  afterEach(() => {
    restoreApi();
  });

  it('offers a new Magic Link when the session is gone', async () => {
    stubApiRoutes([{ method: 'get', path: '/auth/session', respond: unauthorized }]);

    render(AccountStatusFixture);

    expect(await screen.findByText('Sesi tidak ditemukan atau sudah berakhir')).toBeInTheDocument();
    expect(screen.getByText('Masuk dengan Phone Number')).toBeInTheDocument();
  });

  it('shows the active Package, the remaining time, the exact end and a hidden PIN', async () => {
    stubApiRoutes(
      stubsFor(accountSummary, () => ({ data: loadContract('transactions.response.json') })),
    );

    render(AccountStatusFixture);

    expect(await screen.findByText('Sisa Active Period')).toBeInTheDocument();
    expect(screen.getByText('Berakhir')).toBeInTheDocument();
    expect(screen.getByText('Aktif')).toBeInTheDocument();
    expect(screen.getByText('5M')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Extend Package' })).toBeInTheDocument();

    // The PIN is hidden by default: the digits are not in the DOM at all.
    expect(screen.getByTestId('account-pin')).toHaveTextContent('••••••');
    expect(screen.queryByText('123456')).not.toBeInTheDocument();

    await fireEvent.click(screen.getByRole('button', { name: 'Tunjukkan PIN' }));
    expect(screen.getByTestId('account-pin')).toHaveTextContent('123456');
    expect(screen.getByRole('button', { name: 'Sembunyikan PIN' })).toBeInTheDocument();

    // The history renders the state in Indonesian and links to the detail page.
    expect(await screen.findByText('Selesai')).toBeInTheDocument();
    expect(screen.getAllByText('Paket Harian').length).toBeGreaterThan(0);
    expect(screen.getByText('Rp 5.000')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Riwayat|Selesai/ })).toHaveAttribute(
      'href',
      '/transaksi/12',
    );
  });

  it('marks an Active Period inside the warning window as almost over', async () => {
    stubApiRoutes(
      stubsFor(() =>
        accountSummary({
          active_period: {
            expires_at: new Date(Date.now() + 23 * 60 * 60 * 1000).toISOString(),
            found: true,
            state: 'expiring',
          },
        }),
      ),
    );

    render(AccountStatusFixture);

    expect(await screen.findByText('Sisa Active Period')).toBeInTheDocument();
    expect(screen.getAllByText(/Hampir berakhir/).length).toBeGreaterThan(0);
    expect(screen.getByText('Active Period Anda hampir berakhir.')).toBeInTheDocument();
  });

  it('shows an expired Active Period with a way to buy again', async () => {
    stubApiRoutes(
      stubsFor(() =>
        accountSummary({
          active_period: {
            expires_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
            found: true,
            state: 'expired',
          },
        }),
      ),
    );

    render(AccountStatusFixture);

    expect(await screen.findByText('Kedaluwarsa')).toBeInTheDocument();
    expect(screen.getByText(/Active Period Anda sudah habis/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Beli Package baru' })).toBeInTheDocument();
  });

  it('offers a new Magic Link when the summary answers 401 for a revoked session', async () => {
    stubApiRoutes([
      { method: 'get', path: '/auth/session', respond: () => ({ data: session }) },
      { method: 'get', path: '/account', respond: unauthorized },
      { method: 'get', path: '/transactions', respond: emptyHistory },
    ]);

    render(AccountStatusFixture);

    expect(await screen.findByText('Sesi tidak ditemukan atau sudah berakhir')).toBeInTheDocument();
    expect(screen.getByText('Masuk dengan Phone Number')).toBeInTheDocument();
  });

  it('keeps the Active Period and PIN when the catalog lost the Package row', async () => {
    stubApiRoutes(stubsFor(() => accountSummary({ package: null })));

    render(AccountStatusFixture);

    // The expiration is still the buyer's, so this is not the empty state.
    expect(await screen.findByText('Sisa Active Period')).toBeInTheDocument();
    expect(screen.queryByText('Belum ada Active Period')).not.toBeInTheDocument();
    expect(screen.getByTestId('account-pin')).toHaveTextContent('••••••');
  });

  it('points a buyer who never bought anything at the Package catalog', async () => {
    stubApiRoutes(
      stubsFor(() => ({
        data: {
          data: {
            phone_number: '6281234567890',
            active_period: { expires_at: null, found: false, state: 'none' },
            package: null,
            pin: null,
          },
        },
      })),
    );

    render(AccountStatusFixture);

    expect(await screen.findByText('Belum ada Active Period')).toBeInTheDocument();
    expect(await screen.findByText('Paket Harian')).toBeInTheDocument();
    expect(screen.getByText('Belum ada Transaction.')).toBeInTheDocument();
    expect(screen.getAllByRole('link')[0]).toHaveAttribute('href', '/beli?package=1');
  });
});
