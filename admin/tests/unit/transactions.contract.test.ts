import { describe, expect, it } from 'vitest';

import {
  AdminPINRevealResponseSchema,
  AdminTransactionFilterSchema,
  AdminTransactionListResponseSchema,
  AdminTransactionSchema,
  toQueryParams,
} from '$lib/domains/transactions/schemas/transaction.schema';
import { loadContract } from '../support/contracts';

// The same fixture is decoded by the backend integration tests, so a field one
// side forgets fails here or there.
describe('admin transaction contract fixture', () => {
  it('decodes one page of the panel search with its snapshot and SMS status', () => {
    const envelope = AdminTransactionListResponseSchema.parse(
      loadContract('admin-transactions.response.json'),
    );

    expect(envelope.total_count).toBe(1);
    expect(envelope.page).toBe(1);
    expect(envelope.page_size).toBe(20);

    const row = envelope.data[0];
    expect(row.phone_number).toBe('6281234567890');
    // The snapshot, not the current catalog row (ADR 0026).
    expect(row.package_name).toBe('Paket Harian');
    expect(row.amount_idr).toBe(5000);
    // The support answer to "did my PIN arrive?".
    expect(row.last_sms_state).toBe('sent');
    expect(row.provision_attempts).toBe(1);
  });

  it('is strict, so a row that grows a pin fails to parse', () => {
    const fixture = loadContract('admin-transactions.response.json') as {
      data: Record<string, unknown>[];
    };

    // A list must never carry a credential: parsing is what refuses one.
    expect(() => AdminTransactionSchema.parse({ ...fixture.data[0], pin: '123456' })).toThrow();
  });

  it('carries no pin field at all', () => {
    const fixture = loadContract('admin-transactions.response.json') as {
      data: Record<string, unknown>[];
    };

    expect(Object.keys(fixture.data[0])).not.toContain('pin');
  });

  it('accepts a row whose last SMS never happened', () => {
    const fixture = loadContract('admin-transactions.response.json') as {
      data: Record<string, unknown>[];
    };

    const parsed = AdminTransactionSchema.parse({
      ...fixture.data[0],
      last_sms_state: null,
      last_sms_at: null,
    });
    expect(parsed.last_sms_state).toBeNull();
  });
});

describe('admin PIN reveal contract fixture', () => {
  it('decodes the one credential the panel may receive', () => {
    const envelope = AdminPINRevealResponseSchema.parse(
      loadContract('admin-pin-reveal.response.json'),
    );

    expect(envelope.data.pin).toMatch(/^\d{6}$/);
  });

  it('refuses a response that grows a field or is not six digits', () => {
    expect(() =>
      AdminPINRevealResponseSchema.parse({ data: { pin: '123456', ciphertext: 'x' } }),
    ).toThrow();
    expect(() => AdminPINRevealResponseSchema.parse({ data: { pin: '12345' } })).toThrow();
    expect(() => AdminPINRevealResponseSchema.parse({ data: { pin: 123456 } })).toThrow();
  });
});

describe('transaction search form', () => {
  it('drops the unset filters instead of sending empty query values', () => {
    const filter = AdminTransactionFilterSchema.parse({
      page: 2,
      page_size: 20,
      phone_number: '  ',
    });

    expect(toQueryParams(filter)).toEqual({ page: 2, page_size: 20 });
  });

  it('sends the submitted filters, and a day is included as typed', () => {
    const filter = AdminTransactionFilterSchema.parse({
      state: 'provisioned',
      phone_number: '08123456789',
      from: '2026-10-23',
      to: '2026-10-23',
    });

    expect(toQueryParams(filter)).toEqual({
      page: 1,
      page_size: 20,
      state: 'provisioned',
      phone_number: '08123456789',
      from: '2026-10-23',
      to: '2026-10-23',
    });
  });

  it('refuses a state the backend does not know and an oversized page', () => {
    expect(AdminTransactionFilterSchema.safeParse({ state: 'shipped' }).success).toBe(false);
    expect(AdminTransactionFilterSchema.safeParse({ page_size: 101 }).success).toBe(false);
  });
});
