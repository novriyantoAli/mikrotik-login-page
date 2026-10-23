import { fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import MagicLinkFormFixture from '../support/MagicLinkFormFixture.svelte';
import { restoreApi, stubApi } from '../support/api';
import { loadContract } from '../support/contracts';

describe('MagicLinkForm', () => {
  afterEach(() => {
    restoreApi();
  });

  it('asks for a Magic Link and moves to the "check your SMS" state', async () => {
    stubApi(() => ({ status: 202, data: loadContract('magic-link.response.json') }));

    render(MagicLinkFormFixture);

    await fireEvent.input(screen.getByLabelText('Phone Number'), {
      target: { value: '081234567890' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Kirim tautan masuk' }));

    expect(await screen.findByText('Periksa SMS Anda')).toBeInTheDocument();
    expect(screen.getByText('081234567890')).toBeInTheDocument();
  });

  it('explains a rate-limited request without hiding the form', async () => {
    stubApi(() => ({
      status: 429,
      data: {
        error: {
          code: 'rate_limited',
          message: 'Terlalu banyak permintaan dari alamat ini. Coba lagi beberapa saat lagi.',
        },
      },
    }));

    render(MagicLinkFormFixture);

    await fireEvent.input(screen.getByLabelText('Phone Number'), {
      target: { value: '081234567890' },
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Kirim tautan masuk' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Terlalu banyak permintaan');
    expect(screen.getByRole('button', { name: 'Kirim tautan masuk' })).toBeInTheDocument();
  });

  it('refuses to submit an empty Phone Number', async () => {
    render(MagicLinkFormFixture);

    await fireEvent.click(screen.getByRole('button', { name: 'Kirim tautan masuk' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Phone Number wajib diisi.');
    expect(screen.queryByText('Periksa SMS Anda')).not.toBeInTheDocument();
  });
});
