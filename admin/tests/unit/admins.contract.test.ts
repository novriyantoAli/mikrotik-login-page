import { describe, expect, it } from 'vitest';

import {
  AdminListEnvelopeSchema,
  AdminSchema,
  CreateAdminInputSchema,
} from '$lib/domains/admins/schemas/admin.schema';
import { loadContract } from '../support/contracts';

// The same fixture is decoded by the backend integration tests, so the owner,
// support and auditor rows cannot drift apart from the DTO.
describe('admin list contract fixture', () => {
  it('decodes every role', () => {
    const envelope = AdminListEnvelopeSchema.parse(loadContract('admin-admins.response.json'));

    expect(envelope.data.map((admin) => admin.role)).toEqual(['owner', 'support', 'auditor']);
    expect(envelope.data[2].is_active).toBe(false);
  });

  it('rejects a row that grows a password hash', () => {
    const fixture = loadContract('admin-admins.response.json') as {
      data: Record<string, unknown>[];
    };

    expect(() => AdminSchema.parse({ ...fixture.data[0], password_hash: 'leaked' })).toThrow();
  });
});

describe('admin creation rules', () => {
  const valid = {
    email: 'support@example.test',
    role: 'support',
    password: 'kata-sandi-yang-panjang',
  };

  it('accepts a valid colleague', () => {
    expect(CreateAdminInputSchema.safeParse(valid).success).toBe(true);
  });

  it('refuses an invalid email, an unknown role and a weak password', () => {
    expect(CreateAdminInputSchema.safeParse({ ...valid, email: 'bukan-email' }).success).toBe(
      false,
    );
    expect(CreateAdminInputSchema.safeParse({ ...valid, role: 'superuser' }).success).toBe(false);
    expect(CreateAdminInputSchema.safeParse({ ...valid, password: 'pendek' }).success).toBe(false);
  });
});
