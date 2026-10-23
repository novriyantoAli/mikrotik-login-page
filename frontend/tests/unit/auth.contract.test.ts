import { describe, expect, it } from 'vitest';

import {
  ExchangeResultEnvelopeSchema,
  MagicLinkRequestSchema,
  MagicLinkSentEnvelopeSchema,
  SessionEnvelopeSchema,
} from '$lib/domains/auth/schemas/auth.schema';
import { loadContract } from '../support/contracts';

// The same fixtures are decoded by the backend integration tests, so a field one
// side forgets fails here or there. Schemas are strict on purpose.
describe('auth contract fixtures', () => {
  it('decodes the Magic Link request the Login Page triggers', () => {
    const request = MagicLinkRequestSchema.parse(loadContract('magic-link.request.json'));

    expect(request.phone_number).toBe('081234567890');
    expect(request.package_id).toBe(1);
    expect(request.return_url).toContain('192.168.88.1');
  });

  it('decodes the uniform "sent" answer', () => {
    const envelope = MagicLinkSentEnvelopeSchema.parse(loadContract('magic-link.response.json'));

    expect(envelope.data.sent).toBe(true);
  });

  it('decodes a session without ever exposing a token', () => {
    const envelope = SessionEnvelopeSchema.parse(loadContract('session.response.json'));

    expect(envelope.data.phone_number).toBe('6281234567890');
    expect(Object.keys(envelope.data)).not.toContain('token');
  });

  it('decodes the exchange result with the stored intent', () => {
    const envelope = ExchangeResultEnvelopeSchema.parse(
      loadContract('magic-link.exchange.response.json'),
    );

    expect(envelope.data.purpose).toBe('checkout');
    expect(envelope.data.package_id).toBe(1);
  });

  it('rejects a session payload that grows a token field', () => {
    const fixture = loadContract('session.response.json') as {
      data: Record<string, unknown>;
    };

    expect(() =>
      SessionEnvelopeSchema.parse({ data: { ...fixture.data, token: 'leaked' } }),
    ).toThrow();
  });
});
