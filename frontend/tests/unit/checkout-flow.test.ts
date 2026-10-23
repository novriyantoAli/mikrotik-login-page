import { fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import CheckoutFlowFixture from '../support/CheckoutFlowFixture.svelte';
import { restoreApi, stubApiRoutes } from '../support/api';
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

describe('CheckoutFlow', () => {
  afterEach(() => {
    restoreApi();
  });

  it('asks for a Magic Link when there is no session', async () => {
    stubApiRoutes([
      { method: 'get', path: '/auth/session', respond: unauthorized },
      {
        method: 'get',
        path: '/packages',
        respond: () => ({ data: loadContract('packages.response.json') }),
      },
      { method: 'get', path: '/transactions/channels', respond: unauthorized },
    ]);

    render(CheckoutFlowFixture, { packageId: 1 });

    expect(await screen.findByText('Masuk dengan Phone Number')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Lanjut ke pembayaran' })).not.toBeInTheDocument();
  });

  it('walks from the Package to the payment instructions', async () => {
    stubApiRoutes([
      { method: 'get', path: '/auth/session', respond: () => ({ data: session }) },
      {
        method: 'get',
        path: '/packages',
        respond: () => ({ data: loadContract('packages.response.json') }),
      },
      {
        method: 'get',
        path: '/transactions/channels',
        respond: () => ({ data: loadContract('channels.response.json') }),
      },
      {
        method: 'post',
        path: '/transactions',
        respond: () => ({ data: loadContract('transaction.response.json') }),
      },
    ]);

    render(CheckoutFlowFixture, { packageId: 1 });

    // The Package the Login Page handed over is shown before paying.
    expect(await screen.findByText('Paket Harian')).toBeInTheDocument();
    expect(screen.getByText('1 Hari')).toBeInTheDocument();

    const payButton = screen.getByRole('button', { name: 'Lanjut ke pembayaran' });
    expect(payButton).toBeDisabled();

    await fireEvent.click(await screen.findByRole('radio', { name: /BRIVA/ }));
    await fireEvent.click(payButton);

    expect(await screen.findByText('Selesaikan pembayaran')).toBeInTheDocument();
    expect(screen.getByText('8808812345678901')).toBeInTheDocument();
  });

  it('keeps the package visible when the channel list fails, and offers a retry', async () => {
    let attempts = 0;
    stubApiRoutes([
      { method: 'get', path: '/auth/session', respond: () => ({ data: session }) },
      {
        method: 'get',
        path: '/packages',
        respond: () => ({ data: loadContract('packages.response.json') }),
      },
      {
        method: 'get',
        path: '/transactions/channels',
        respond: () => {
          attempts += 1;
          if (attempts === 1) {
            return {
              status: 502,
              data: {
                error: {
                  code: 'dependency_unavailable',
                  message: 'Pembayaran sedang tidak dapat diproses. Coba lagi beberapa saat lagi.',
                },
              },
            };
          }
          return { data: loadContract('channels.response.json') };
        },
      },
    ]);

    render(CheckoutFlowFixture, { packageId: 1 });

    expect(await screen.findByText('Kanal pembayaran tidak dapat dimuat')).toBeInTheDocument();
    expect(screen.getByText('Paket Harian')).toBeInTheDocument();

    await fireEvent.click(screen.getByRole('button', { name: 'Coba lagi' }));
    expect(await screen.findByRole('radio', { name: /BRIVA/ })).toBeInTheDocument();
  });
});
