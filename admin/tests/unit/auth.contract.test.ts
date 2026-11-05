import { describe, expect, it } from 'vitest';

import {
  AdminPasswordSchema,
  LoginInputSchema,
  SessionEnvelopeSchema,
} from '$lib/domains/auth/schemas/auth.schema';
import { loadContract } from '../support/contracts';

// The same fixtures are decoded by the backend integration tests, so a field one
// side forgets fails here or there. Schemas are strict on purpose.
describe('admin auth contract fixtures', () => {
  it('decodes the sign-in body the panel sends', () => {
    const request = LoginInputSchema.parse(loadContract('admin-login.request.json'));

    expect(request.email).toBe('owner@example.test');
    expect(AdminPasswordSchema.safeParse(request.password).success).toBe(true);
  });

  it('decodes the session without ever exposing a token', () => {
    const envelope = SessionEnvelopeSchema.parse(loadContract('admin-session.response.json'));

    expect(envelope.data.email).toBe('owner@example.test');
    expect(envelope.data.role).toBe('owner');
    expect(Object.keys(envelope.data)).not.toContain('token');
  });

  it('rejects a session payload that grows a token field', () => {
    const fixture = loadContract('admin-session.response.json') as {
      data: Record<string, unknown>;
    };

    expect(() =>
      SessionEnvelopeSchema.parse({ data: { ...fixture.data, token: 'leaked' } }),
    ).toThrow();
  });

  it('rejects an unknown role and a weak password', () => {
    const fixture = loadContract('admin-session.response.json') as {
      data: Record<string, unknown>;
    };

    expect(() =>
      SessionEnvelopeSchema.parse({ data: { ...fixture.data, role: 'superuser' } }),
    ).toThrow();
    expect(AdminPasswordSchema.safeParse('pendek').success).toBe(false);
  });
});
