import { render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import MagicLinkRedeemFixture from '../support/MagicLinkRedeemFixture.svelte';
import { restoreApi, stubApi } from '../support/api';
import { loadContract } from '../support/contracts';

describe('MagicLinkRedeem', () => {
  afterEach(() => {
    restoreApi();
  });

  it('opens the session and offers to continue to the Login Page', async () => {
    stubApi(() => ({ status: 200, data: loadContract('magic-link.exchange.response.json') }));

    render(MagicLinkRedeemFixture);

    expect(await screen.findByText('Berhasil masuk')).toBeInTheDocument();
    expect(screen.getByText('6281234567890')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Lanjut' })).toBeInTheDocument();
  });

  it('never offers to retry a spent link, only a new one', async () => {
    stubApi(() => ({
      status: 400,
      data: {
        error: { code: 'magic_link_invalid', message: 'Tautan tidak berlaku. Minta tautan baru.' },
      },
    }));

    render(MagicLinkRedeemFixture);

    expect(await screen.findByText('Tautan tidak bisa dipakai')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Coba lagi' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Minta tautan baru' })).toBeInTheDocument();
  });

  it('offers a retry when the backend was unreachable', async () => {
    stubApi(() => ({
      status: 500,
      data: { error: { code: 'internal_error', message: 'Terjadi kesalahan pada server.' } },
    }));

    render(MagicLinkRedeemFixture);

    expect(await screen.findByRole('button', { name: 'Coba lagi' })).toBeInTheDocument();
  });
});
