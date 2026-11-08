import { describe, expect, it } from 'vitest';

import {
  AdminAuditEntrySchema,
  AdminAuditFilterSchema,
  AdminAuditListResponseSchema,
  auditPayloadFields,
  formatAuditValue,
  toAuditQueryParams,
} from '$lib/domains/audit';
import { loadContract } from '../support/contracts';

// The same fixture is decoded by the backend integration tests, so a field one side
// forgets fails here or there.
describe('admin audit contract fixture', () => {
  it('decodes one page of the trail with a catalog edit and a reveal', () => {
    const envelope = AdminAuditListResponseSchema.parse(loadContract('admin-audit.response.json'));

    expect(envelope.total_count).toBe(2);
    expect(envelope.page).toBe(1);
    expect(envelope.page_size).toBe(20);

    const [edit, reveal] = envelope.data;
    expect(edit.action).toBe('package.update');
    expect(edit.target_type).toBe('package');
    // The catalog criterion: the price reads as before → after.
    expect(edit.payload.price_idr).toEqual({ before: 5000, after: 7500 });

    expect(reveal.action).toBe('pin.reveal');
    expect(reveal.target_type).toBe('transaction');
    expect(reveal.target_id).toBe('12');
    expect(reveal.payload).toEqual({ result: 'ok', reference: 'TRX-0123456789ABCDEF' });
  });

  it('carries no plaintext PIN anywhere in the reveal row', () => {
    const fixture = loadContract('admin-audit.response.json') as {
      data: Record<string, unknown>[];
    };

    const reveal = fixture.data[1];
    expect(Object.keys(reveal)).not.toContain('pin');
    expect(JSON.stringify(reveal)).not.toMatch(/\b\d{6}\b/);
  });

  it('is strict on the envelope and the columns, so an unknown field fails the read', () => {
    const fixture = loadContract('admin-audit.response.json') as {
      data: Record<string, unknown>[];
    };

    expect(() => AdminAuditEntrySchema.parse({ ...fixture.data[0], pin: '123456' })).toThrow();
    expect(() =>
      AdminAuditListResponseSchema.parse({
        data: fixture.data,
        total_count: 2,
        page: 1,
        page_size: 20,
        deleted: true,
      }),
    ).toThrow();
  });

  it('keeps the payload open, so a new action field does not break the reader', () => {
    const fixture = loadContract('admin-audit.response.json') as {
      data: Record<string, unknown>[];
    };

    const parsed = AdminAuditEntrySchema.parse({
      ...fixture.data[0],
      payload: { ...(fixture.data[0].payload as object), something_new: 'nilai' },
    });
    expect(parsed.payload.something_new).toBe('nilai');
  });

  it('renders an action this build does not know as itself', () => {
    const fixture = loadContract('admin-audit.response.json') as {
      data: Record<string, unknown>[];
    };

    // The column is a string, not an enum: a newer backend action must still read.
    expect(() =>
      AdminAuditEntrySchema.parse({ ...fixture.data[0], action: 'package.reprice' }),
    ).not.toThrow();
  });
});

describe('audit payload rendering', () => {
  it('turns a before/after object into a change row with human labels', () => {
    const fields = auditPayloadFields({
      price_idr: { before: 5000, after: 7500 },
      version: { before: 1, after: 2 },
    });

    expect(fields).toEqual([
      { key: 'price_idr', label: 'Harga', before: 5000, after: 7500, change: true },
      { key: 'version', label: 'Versi', before: 1, after: 2, change: true },
    ]);
  });

  it('leaves a flat field as one value, and never invents a before', () => {
    const fields = auditPayloadFields({
      after: { code: 'paket-harian', is_active: true },
      result: 'ok',
    });

    const flat = fields.find((field) => field.key === 'result');
    expect(flat?.change).toBe(false);
    expect(flat?.value).toBe('ok');
    expect(flat?.before).toBeUndefined();
  });

  it('formats booleans, absent values and objects as copy', () => {
    expect(formatAuditValue(true)).toBe('ya');
    expect(formatAuditValue(false)).toBe('tidak');
    expect(formatAuditValue(null)).toBe('—');
    expect(formatAuditValue(undefined)).toBe('—');
    expect(formatAuditValue(7500)).toBe('7500');
    expect(formatAuditValue({ code: 'x' })).toBe('{"code":"x"}');
  });
});

describe('audit search form', () => {
  it('drops the unset filters instead of sending empty query values', () => {
    const filter = AdminAuditFilterSchema.parse({ admin_email: '  ' });

    expect(toAuditQueryParams(filter)).toEqual({ page: 1, page_size: 20 });
  });

  it('sends the submitted filters, and a day is included as typed', () => {
    const filter = AdminAuditFilterSchema.parse({
      admin_email: 'owner@example.test',
      action: 'package.update',
      from: '2026-10-23',
      to: '2026-10-24',
    });

    expect(toAuditQueryParams(filter)).toEqual({
      page: 1,
      page_size: 20,
      admin_email: 'owner@example.test',
      action: 'package.update',
      from: '2026-10-23',
      to: '2026-10-24',
    });
  });

  it('refuses an action this build does not offer and an oversized page', () => {
    expect(AdminAuditFilterSchema.safeParse({ action: 'package.explode' }).success).toBe(false);
    expect(AdminAuditFilterSchema.safeParse({ page_size: 101 }).success).toBe(false);
  });
});
