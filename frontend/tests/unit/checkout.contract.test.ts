import { describe, expect, it } from 'vitest';

import {
  ChannelListEnvelopeSchema,
  CreateTransactionInputSchema,
  TransactionEnvelopeSchema,
  isTerminalState,
  pollingInterval,
} from '$lib/domains/checkout';
import { loadContract } from '../support/contracts';

// These fixtures are decoded by the backend integration tests with
// DisallowUnknownFields, so a field either side forgets fails here or there.
describe('checkout contract fixtures', () => {
  it('decodes the channel list with fees separate from the price', () => {
    const envelope = ChannelListEnvelopeSchema.parse(loadContract('channels.response.json'));

    expect(envelope.data.map((channel) => channel.code)).toEqual(['BRIVA', 'QRIS']);
    expect(envelope.data[0].fee_flat_idr).toBe(2500);
    expect(envelope.data[1].fee_percent).toBe(0.7);
  });

  it('decodes the checkout request the app sends', () => {
    const request = CreateTransactionInputSchema.parse(
      loadContract('transaction.create.request.json'),
    );

    expect(request.package_id).toBe(1);
    expect(request.channel).toBe('BRIVA');
    expect(request.return_url).toContain('192.168.88.1');
  });

  it('decodes a pending Transaction with its instructions', () => {
    const envelope = TransactionEnvelopeSchema.parse(loadContract('transaction.response.json'));

    expect(envelope.data.state).toBe('pending');
    expect(envelope.data.pay_code).not.toBe('');
    expect(envelope.data.instructions[0].steps.length).toBeGreaterThan(1);
    expect(envelope.data.paid_at).toBeNull();
  });

  it('decodes the paid state of the same Transaction', () => {
    const envelope = TransactionEnvelopeSchema.parse(
      loadContract('transaction.paid.response.json'),
    );

    expect(envelope.data.state).toBe('paid');
    expect(envelope.data.paid_at).not.toBeNull();
  });

  it('rejects a Transaction payload that grows an unknown field', () => {
    const fixture = loadContract('transaction.response.json') as {
      data: Record<string, unknown>;
    };

    expect(() =>
      TransactionEnvelopeSchema.parse({ data: { ...fixture.data, amount_float: 5000.5 } }),
    ).toThrow();
  });
});

describe('polling rules', () => {
  it('polls while pending and the tab is visible', () => {
    expect(pollingInterval('pending', false)).toBe(4000);
  });

  it('stops at every terminal state', () => {
    for (const state of ['paid', 'provisioned', 'failed', 'expired'] as const) {
      expect(isTerminalState(state)).toBe(true);
      expect(pollingInterval(state, false)).toBe(false);
    }
  });

  it('stops while the tab is hidden, even when the payment is still pending', () => {
    expect(pollingInterval('pending', true)).toBe(false);
  });
});
