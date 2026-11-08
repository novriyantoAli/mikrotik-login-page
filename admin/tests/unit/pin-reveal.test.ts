import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it } from 'vitest';

import { AdminTransactionSchema } from '$lib/domains/transactions';
import TransactionDetailFixture from '../support/TransactionDetailFixture.svelte';
import { restoreApi, stubApiRoutes, type StubRoute } from '../support/api';
import { loadContract } from '../support/contracts';

/** The row the panel searched, decoded through the domain's own schema. */
function provisioned() {
  const fixture = loadContract('admin-transactions.response.json') as {
    data: unknown[];
  };
  return AdminTransactionSchema.parse(fixture.data[0]);
}

/** The audited reveal's answer, straight from the shared fixture. */
const revealAnswer = () => ({ status: 200, data: loadContract('admin-pin-reveal.response.json') });

function stubReveal(respond: StubRoute['respond']): void {
  stubApiRoutes([{ method: 'post', path: /^\/admin\/transactions\/\d+\/pin$/, respond }]);
}

describe('PIN reveal', () => {
  afterEach(() => {
    restoreApi();
  });

  it('shows no PIN until support asks, and warns that asking is recorded', () => {
    render(TransactionDetailFixture, { transaction: provisioned(), canReveal: true });

    expect(screen.getByText(/tercatat di audit log/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Buka PIN' })).toBeInTheDocument();
    // Nothing is revealed by opening the row: the list and the detail carry no digits.
    expect(screen.queryByTestId('revealed-pin')).not.toBeInTheDocument();
  });

  it('reveals the PIN from the contract fixture and can hide it again', async () => {
    stubReveal(revealAnswer);

    render(TransactionDetailFixture, { transaction: provisioned(), canReveal: true });
    await fireEvent.click(screen.getByRole('button', { name: 'Buka PIN' }));

    const shown = await screen.findByTestId('revealed-pin');
    expect(shown).toHaveTextContent('123456');

    await fireEvent.click(screen.getByRole('button', { name: 'Sembunyikan' }));
    await waitFor(() => expect(screen.getByTestId('revealed-pin')).toHaveTextContent('••••••'));

    await fireEvent.click(screen.getByRole('button', { name: 'Tampilkan' }));
    await waitFor(() => expect(screen.getByTestId('revealed-pin')).toHaveTextContent('123456'));
  });

  it('forgets the PIN when the operator closes the panel', async () => {
    stubReveal(revealAnswer);

    render(TransactionDetailFixture, { transaction: provisioned(), canReveal: true });
    await fireEvent.click(screen.getByRole('button', { name: 'Buka PIN' }));
    await screen.findByTestId('revealed-pin');

    await fireEvent.click(screen.getByRole('button', { name: 'Tutup' }));

    // Back to the unrevealed state: the value is gone, not merely hidden.
    await waitFor(() => expect(screen.queryByTestId('revealed-pin')).not.toBeInTheDocument());
    expect(screen.getByRole('button', { name: 'Buka PIN' })).toBeInTheDocument();
  });

  it('says a refusal out loud and never shows digits', async () => {
    stubReveal(() => ({
      status: 429,
      data: {
        error: {
          code: 'rate_limited',
          message: 'Terlalu banyak permintaan membuka PIN dalam waktu singkat. Coba lagi nanti.',
        },
      },
    }));

    render(TransactionDetailFixture, { transaction: provisioned(), canReveal: true });
    await fireEvent.click(screen.getByRole('button', { name: 'Buka PIN' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/terlalu banyak permintaan/i);
    expect(screen.queryByTestId('revealed-pin')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Coba lagi' })).toBeInTheDocument();
  });

  it('fails the reveal when the response is not the contract', async () => {
    // A backend that answered extra fields or a non-PIN value must not reach the screen.
    stubReveal(() => ({ status: 200, data: { data: { pin: '12345', leaked: true } } }));

    render(TransactionDetailFixture, { transaction: provisioned(), canReveal: true });
    await fireEvent.click(screen.getByRole('button', { name: 'Buka PIN' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/kontrak panel/i);
    expect(screen.queryByTestId('revealed-pin')).not.toBeInTheDocument();
  });

  it('hides the action from a role that may not open a PIN', () => {
    render(TransactionDetailFixture, { transaction: provisioned(), canReveal: false });

    expect(screen.queryByRole('button', { name: 'Buka PIN' })).not.toBeInTheDocument();
    expect(screen.getByText(/tidak boleh membuka PIN/i)).toBeInTheDocument();
  });

  it('explains that a paid Transaction has no PIN yet', () => {
    render(TransactionDetailFixture, {
      transaction: { ...provisioned(), state: 'paid' },
      canReveal: true,
    });

    expect(screen.queryByRole('button', { name: 'Buka PIN' })).not.toBeInTheDocument();
    expect(screen.getByText(/selesai provisioning/i)).toBeInTheDocument();
  });
});
