import { describe, expect, it } from 'vitest';

import {
  AccountEnvelopeSchema,
  ActivePeriodStateSchema,
  ExtendPreviewEnvelopeSchema,
  TransactionHistorySchema,
} from '$lib/domains/account';
import { loadContract } from '../support/contracts';

// These fixtures are decoded by the backend integration tests with
// DisallowUnknownFields, so a field either side forgets fails here or there.
describe('Account Status contract fixtures', () => {
  it('decodes the summary with the Active Period, the Package and the PIN', () => {
    const account = AccountEnvelopeSchema.parse(loadContract('account.response.json')).data;

    expect(account.phone_number).toBe('6281234567890');
    expect(account.active_period.found).toBe(true);
    expect(account.active_period.state).toBe('active');
    expect(account.package?.rate_limit).toBe('5M/5M');
    expect(account.pin).toBe('123456');
  });

  it('decodes the Extend preview with its estimate', () => {
    const preview = ExtendPreviewEnvelopeSchema.parse(
      loadContract('account.extend-preview.response.json'),
    ).data;

    expect(preview.extends).toBe(true);
    expect(preview.current_expiration).toBe('2026-10-24T08:00:00Z');
    expect(preview.estimated_expiration).toBe('2026-10-25T08:00:00Z');
  });

  it('decodes the paginated history without any PIN', () => {
    const history = TransactionHistorySchema.parse(loadContract('transactions.response.json'));

    expect(history.total_count).toBe(1);
    expect(history.page).toBe(1);
    expect(history.page_size).toBe(20);
    expect(history.data[0].state).toBe('provisioned');
    expect(history.data[0].amount_idr).toBe(5000);
  });

  it('rejects a history row that grows a pin field', () => {
    const fixture = loadContract('transactions.response.json') as {
      data: Record<string, unknown>[];
    };
    const [row] = fixture.data;

    expect(() =>
      TransactionHistorySchema.parse({ ...fixture, data: [{ ...row, pin: '123456' }] }),
    ).toThrow();
  });

  it('rejects a summary that grows an unknown field', () => {
    const fixture = loadContract('account.response.json') as { data: Record<string, unknown> };

    expect(() =>
      AccountEnvelopeSchema.parse({ data: { ...fixture.data, balance_idr: 5000 } }),
    ).toThrow();
  });

  it('accepts every Active Period state the backend can send and nothing else', () => {
    for (const state of ['active', 'expiring', 'expired', 'none'] as const) {
      expect(ActivePeriodStateSchema.parse(state)).toBe(state);
    }
    expect(() => ActivePeriodStateSchema.parse('almost')).toThrow();
  });
});
