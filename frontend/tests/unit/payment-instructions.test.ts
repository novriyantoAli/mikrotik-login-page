import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';

import { ChannelListEnvelopeSchema, TransactionEnvelopeSchema } from '$lib/domains/checkout';
import PaymentInstructionsFixture from '../support/PaymentInstructionsFixture.svelte';
import { loadContract } from '../support/contracts';

const pending = TransactionEnvelopeSchema.parse(loadContract('transaction.response.json')).data;
const paid = TransactionEnvelopeSchema.parse(loadContract('transaction.paid.response.json')).data;
const briva = ChannelListEnvelopeSchema.parse(loadContract('channels.response.json')).data[0];

describe('PaymentInstructions', () => {
  it('renders the pay code, the amount, the channel fee and the deadline', () => {
    render(PaymentInstructionsFixture, { transaction: pending, channel: briva });

    expect(screen.getByText('Selesaikan pembayaran')).toBeInTheDocument();
    expect(screen.getByText('8808812345678901')).toBeInTheDocument();
    expect(screen.getByText('Rp 5.000')).toBeInTheDocument();
    expect(screen.getByText('Biaya kanal')).toBeInTheDocument();
    expect(screen.getByText('Rp 2.500')).toBeInTheDocument();
    expect(screen.getByText(/Bayar sebelum/)).toBeInTheDocument();
    expect(screen.getByText('Masukkan kode bayar')).toBeInTheDocument();
  });

  it('tells the buyer the state updates itself while pending', () => {
    render(PaymentInstructionsFixture, { transaction: pending, channel: briva });

    expect(screen.getByRole('status')).toHaveTextContent('Menunggu pembayaran');
  });

  it('offers a new Transaction once the deadline passed', async () => {
    const onNewTransaction = vi.fn();
    render(PaymentInstructionsFixture, {
      transaction: { ...pending, state: 'expired' },
      onNewTransaction,
    });

    expect(screen.getByText('Waktu pembayaran habis')).toBeInTheDocument();
    expect(screen.queryByText('8808812345678901')).not.toBeInTheDocument();

    await fireEvent.click(screen.getByRole('button', { name: 'Buat Transaction baru' }));
    expect(onNewTransaction).toHaveBeenCalledOnce();
  });

  it('confirms a settled payment instead of showing a pay code', () => {
    render(PaymentInstructionsFixture, { transaction: paid });

    expect(screen.getByText('Pembayaran diterima')).toBeInTheDocument();
    expect(screen.queryByText('Kode bayar')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Kembali ke Login Page' })).toBeInTheDocument();
  });
});
