import { describe, expect, it } from 'vitest';

import { PackageListEnvelopeSchema } from '$lib/domains/packages';
import { loadContract } from '../support/contracts';

// The same fixture is decoded by the backend integration tests, so the frozen
// Login Page contract cannot drift on either side.
describe('Package contract fixture', () => {
  it('decodes the frozen Login Page catalog', () => {
    const envelope = PackageListEnvelopeSchema.parse(loadContract('packages.response.json'));

    expect(envelope.data.length).toBeGreaterThan(0);
    expect(envelope.data[0]).toMatchObject({
      id: expect.any(Number),
      nama: expect.any(String),
      harga: expect.any(String),
      durasiActivePeriod: expect.any(String),
      batasKecepatan: expect.any(String),
    });
  });

  it('rejects a catalog entry that grows a typed price field', () => {
    const fixture = loadContract('packages.response.json') as {
      data: Record<string, unknown>[];
    };

    expect(() =>
      PackageListEnvelopeSchema.parse({
        data: [{ ...fixture.data[0], price_idr: 5000 }],
      }),
    ).toThrow();
  });
});
