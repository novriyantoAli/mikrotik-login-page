import { render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import { formatDateTime } from '$lib/domains/account';

import CheckoutFlowFixture from '../support/CheckoutFlowFixture.svelte';
import { restoreApi, stubApiRoutes } from '../support/api';
import { loadContract } from '../support/contracts';

const session = { data: { phone_number: '6281234567890', expires_at: '2026-11-22T08:00:00Z' } };

function stubsFor(activePeriod: Record<string, unknown>) {
  return [
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
      method: 'get',
      path: '/account',
      respond: () => ({
        data: {
          data: {
            phone_number: '6281234567890',
            active_period: activePeriod,
            package: { id: 1, name: 'Paket Harian', rate_limit: '5M/5M' },
            pin: '123456',
          },
        },
      }),
    },
    {
      method: 'get',
      path: '/account/extend-preview',
      respond: () => ({ data: loadContract('account.extend-preview.response.json') }),
    },
  ];
}

describe('Extend confirmation', () => {
  afterEach(() => {
    restoreApi();
  });

  it('confirms an Extend with the estimated new expiration before paying', async () => {
    const currentExpiration = new Date(Date.now() + 6 * 60 * 60 * 1000).toISOString();
    stubApiRoutes(stubsFor({ expires_at: currentExpiration, found: true, state: 'active' }));

    render(CheckoutFlowFixture, { packageId: 1 });

    expect(await screen.findByText('Extend Active Period')).toBeInTheDocument();
    expect(screen.getByText(/menambah durasi Package ke sisa masa aktif/)).toBeInTheDocument();
    expect(await screen.findByTestId('extend-estimate')).toHaveTextContent(
      formatDateTime('2026-10-25T08:00:00Z'),
    );
    // The buyer still has to pick a channel and press pay: the confirmation only
    // informs what the purchase will do (PRD point 21).
    expect(screen.getByRole('button', { name: 'Lanjut ke pembayaran' })).toBeInTheDocument();
  });

  it('does not call a purchase an Extend when the Active Period already ended', async () => {
    stubApiRoutes(
      stubsFor({
        expires_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
        found: true,
        state: 'expired',
      }),
    );

    render(CheckoutFlowFixture, { packageId: 1 });

    expect(await screen.findByText('Paket Harian')).toBeInTheDocument();
    expect(screen.queryByText('Extend Active Period')).not.toBeInTheDocument();
  });
});
