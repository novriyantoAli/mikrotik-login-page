import { describe, expect, it } from 'vitest';

import {
  AdminPackageListEnvelopeSchema,
  AdminPackageSchema,
  CreatePackageInputSchema,
  UpdatePackageInputSchema,
  readVersionConflict,
} from '$lib/domains/packages/schemas/package.schema';
import { loadContract } from '../support/contracts';

// This fixture is also decoded by the backend integration tests, so a field one
// side forgets fails here or there.
describe('admin package contract fixture', () => {
  it('decodes the panel catalog with typed values and versions', () => {
    const envelope = AdminPackageListEnvelopeSchema.parse(
      loadContract('admin-packages.response.json'),
    );

    expect(envelope.data).toHaveLength(1);
    expect(envelope.data[0].price_idr).toBe(5000);
    expect(envelope.data[0].duration_minutes).toBe(1440);
    expect(envelope.data[0].is_active).toBe(true);
    expect(envelope.data[0].version).toBe(1);
  });

  it('rejects a row that grows an unknown field', () => {
    const fixture = loadContract('admin-packages.response.json') as {
      data: Record<string, unknown>[];
    };

    expect(() => AdminPackageSchema.parse({ ...fixture.data[0], cost_usd: 1 })).toThrow();
  });
});

describe('package form rules', () => {
  const valid = {
    code: 'harian',
    name: 'Paket Harian',
    price_idr: 5000,
    duration_minutes: 1440,
    rate_limit: '5M/5M',
  };

  it('accepts the bounds the ADR settled', () => {
    expect(CreatePackageInputSchema.safeParse(valid).success).toBe(true);
    expect(
      CreatePackageInputSchema.safeParse({
        ...valid,
        price_idr: 100_000_000,
        duration_minutes: 527_040,
      }).success,
    ).toBe(true);
  });

  it('refuses a zero price, a zero duration, an empty name and a bad rate limit', () => {
    expect(CreatePackageInputSchema.safeParse({ ...valid, price_idr: 0 }).success).toBe(false);
    expect(CreatePackageInputSchema.safeParse({ ...valid, duration_minutes: 0 }).success).toBe(
      false,
    );
    expect(CreatePackageInputSchema.safeParse({ ...valid, name: '   ' }).success).toBe(false);
    expect(CreatePackageInputSchema.safeParse({ ...valid, rate_limit: '5M' }).success).toBe(false);
  });

  it('refuses a code that is not the seed identity format', () => {
    expect(CreatePackageInputSchema.safeParse({ ...valid, code: 'Paket Harian' }).success).toBe(
      false,
    );
    expect(CreatePackageInputSchema.safeParse({ ...valid, code: 'harian-' }).success).toBe(false);
  });

  it('requires the version an edit was based on', () => {
    const { code, ...fields } = valid;
    expect(UpdatePackageInputSchema.safeParse(fields).success).toBe(false);
    expect(UpdatePackageInputSchema.safeParse({ ...fields, code, version: 1 }).success).toBe(true);
  });
});

describe('stale-edit conflict body', () => {
  it('reads the current row out of a 409', () => {
    const fixture = loadContract('admin-packages.response.json') as {
      data: Record<string, unknown>[];
    };
    const current = { ...fixture.data[0], name: 'Paket Harian Baru', price_idr: 7000, version: 2 };

    const conflict = readVersionConflict({
      error: {
        code: 'conflict',
        message: 'Paket sudah diubah orang lain.',
        current,
      },
    });

    expect(conflict?.current.name).toBe('Paket Harian Baru');
    expect(conflict?.current.version).toBe(2);
  });

  it('answers null for any other failure body', () => {
    expect(readVersionConflict({ error: { code: 'not_found', message: 'x' } })).toBeNull();
    expect(readVersionConflict(undefined)).toBeNull();
  });
});
