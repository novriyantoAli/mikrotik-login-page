import { afterEach, describe, expect, it } from 'vitest';

import { authApi } from '$lib/domains/auth/api/auth.api';
import { restoreApi, stubApi, stubNetworkFailure } from '../support/api';
import { loadContract } from '../support/contracts';

describe('auth api', () => {
  afterEach(() => {
    restoreApi();
  });

  it('asks for a Magic Link and reports it as queued', async () => {
    stubApi(() => ({ status: 202, data: loadContract('magic-link.response.json') }));

    const result = await authApi.requestMagicLink({ phone_number: '081234567890', package_id: 1 });

    expect(result.sent).toBe(true);
  });

  it('rejects a request the schema does not accept before it leaves the browser', async () => {
    stubApi(() => ({ status: 202, data: loadContract('magic-link.response.json') }));

    await expect(
      authApi.requestMagicLink({ phone_number: '', package_id: 1 }),
    ).rejects.toBeTruthy();
  });

  it('treats a 401 as "no session" instead of an error', async () => {
    stubApi(() => ({
      status: 401,
      data: { error: { code: 'unauthorized', message: 'Sesi tidak ditemukan.' } },
    }));

    await expect(authApi.session()).resolves.toBeNull();
  });

  it('returns the session identity', async () => {
    stubApi(() => ({ status: 200, data: loadContract('session.response.json') }));

    await expect(authApi.session()).resolves.toMatchObject({
      phone_number: '6281234567890',
    });
  });

  it('parses the exchange result through the schema', async () => {
    stubApi(() => ({ status: 200, data: loadContract('magic-link.exchange.response.json') }));

    await expect(authApi.exchange('token-from-sms')).resolves.toMatchObject({
      purpose: 'checkout',
      phone_number: '6281234567890',
    });
  });

  it('keeps a rejected link readable for the UI', async () => {
    stubApi(() => ({
      status: 400,
      data: {
        error: {
          code: 'magic_link_invalid',
          message: 'Tautan tidak berlaku. Minta tautan baru.',
        },
      },
    }));

    await expect(authApi.exchange('stale')).rejects.toMatchObject({
      message: expect.stringContaining('Tautan tidak berlaku'),
      code: 'magic_link_invalid',
    });
  });

  it('reports an unreachable backend in Indonesian', async () => {
    stubNetworkFailure();

    await expect(authApi.requestMagicLink({ phone_number: '081234567890' })).rejects.toMatchObject({
      message: expect.stringContaining('Tidak dapat menghubungi server'),
    });
  });

  it('does not leak a schema failure as a raw zod error', async () => {
    stubApi(() => ({ status: 200, data: { data: { phone_number: '6281234567890' } } }));

    await expect(authApi.session()).rejects.toBeTruthy();
  });
});
