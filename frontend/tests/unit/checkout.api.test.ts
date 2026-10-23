import { afterEach, describe, expect, it } from 'vitest';

import { checkoutApi } from '$lib/domains/checkout/api/checkout.api';
import { packageApi } from '$lib/domains/packages/api/package.api';
import { restoreApi, stubApi } from '../support/api';
import { loadContract } from '../support/contracts';

describe('checkoutApi', () => {
  afterEach(() => {
    restoreApi();
  });

  it('parses the channel list through the schema', async () => {
    stubApi(() => ({ data: loadContract('channels.response.json') }));

    const channels = await checkoutApi.channels();
    expect(channels[0].code).toBe('BRIVA');
  });

  it('refuses a channel payload the schema does not model', async () => {
    stubApi(() => ({
      data: {
        data: [
          { code: 'BRIVA', name: 'BRIVA', group: '', fee_flat_idr: 0, fee_percent: 0, icon: 'x' },
        ],
      },
    }));

    await expect(checkoutApi.channels()).rejects.toThrow();
  });

  it('creates a Transaction and returns the parsed instructions', async () => {
    stubApi(() => ({ data: loadContract('transaction.response.json') }));

    const transaction = await checkoutApi.create({ package_id: 1, channel: 'BRIVA' });
    expect(transaction.state).toBe('pending');
    expect(transaction.pay_code).toBe('8808812345678901');
  });

  it('rejects an invalid request before calling the backend', async () => {
    stubApi(() => ({ data: loadContract('transaction.response.json') }));

    await expect(checkoutApi.create({ package_id: 0, channel: '' })).rejects.toThrow();
  });

  it('reads one Transaction for polling', async () => {
    stubApi(() => ({ data: loadContract('transaction.paid.response.json') }));

    const transaction = await checkoutApi.get(12);
    expect(transaction.state).toBe('paid');
    expect(transaction.paid_at).not.toBeNull();
  });
});

describe('packageApi', () => {
  afterEach(() => {
    restoreApi();
  });

  it('parses the public catalog', async () => {
    stubApi(() => ({ data: loadContract('packages.response.json') }));

    const packages = await packageApi.list();
    expect(packages.map((pkg) => pkg.id)).toEqual([1, 2]);
  });
});
