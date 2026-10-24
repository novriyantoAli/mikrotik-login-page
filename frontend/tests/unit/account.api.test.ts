import { afterEach, describe, expect, it } from 'vitest';

import { accountApi } from '$lib/domains/account/api/account.api';
import { loadContract } from '../support/contracts';
import { restoreApi, stubApi, stubApiRoutes } from '../support/api';

describe('accountApi', () => {
  afterEach(() => {
    restoreApi();
  });

  it('reads the summary and parses it through the strict schema', async () => {
    stubApi(() => ({ data: loadContract('account.response.json') }));

    const account = await accountApi.summary();
    expect(account.phone_number).toBe('6281234567890');
    expect(account.pin).toBe('123456');
  });

  it('fails loudly when the summary grows an unknown field', async () => {
    const fixture = loadContract('account.response.json') as { data: Record<string, unknown> };
    stubApi(() => ({ data: { data: { ...fixture.data, extra: true } } }));

    await expect(accountApi.summary()).rejects.toThrow();
  });

  it('asks for one history page with the pagination parameters', async () => {
    const seen: { url?: string; params?: unknown } = {};
    stubApiRoutes([
      {
        method: 'get',
        path: '/transactions',
        respond: () => ({ data: loadContract('transactions.response.json') }),
      },
    ]);
    // The stub router ignores params, so read them directly from the hook.
    const { apiClient } = await import('$lib/api/client');
    const original = apiClient.interceptors.request.use((config) => {
      seen.url = config.url;
      seen.params = config.params;
      return config;
    });

    const history = await accountApi.history(2, 5);
    apiClient.interceptors.request.eject(original);

    expect(history.total_count).toBe(1);
    expect(seen.url).toBe('/transactions');
    expect(seen.params).toEqual({ page: 2, page_size: 5 });
  });

  it('reads the Extend preview for the selected Package', async () => {
    stubApi(() => ({ data: loadContract('account.extend-preview.response.json') }));

    const preview = await accountApi.extendPreview(1);
    expect(preview.extends).toBe(true);
    expect(preview.estimated_expiration).toBe('2026-10-25T08:00:00Z');
  });
});
