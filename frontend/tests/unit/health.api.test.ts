import { afterEach, describe, expect, it } from 'vitest';

import { healthApi } from '$lib/domains/health';
import { restoreApi, stubApi, stubNetworkFailure } from '../support/api';
import { loadContract } from '../support/contracts';

describe('health api', () => {
  afterEach(() => {
    restoreApi();
  });

  it('returns the report from a healthy backend', async () => {
    stubApi(() => ({ status: 200, data: loadContract('health.response.json') }));

    const health = await healthApi.get();

    expect(health.status).toBe('ok');
    expect(health.checks.postgres.latency_ms).toBe(3);
  });

  it('treats a 503 as a degraded report instead of a failure', async () => {
    stubApi(() => ({ status: 503, data: loadContract('health.degraded.response.json') }));

    const health = await healthApi.get();

    expect(health.status).toBe('degraded');
    expect(health.checks.freeradius_mysql.status).toBe('down');
    if (health.checks.freeradius_mysql.status === 'down') {
      expect(health.checks.freeradius_mysql.error).toBe('unreachable');
    }
  });

  it('rejects with a readable message when the backend is unreachable', async () => {
    stubNetworkFailure();

    await expect(healthApi.get()).rejects.toMatchObject({
      message: expect.stringContaining('Tidak dapat menghubungi server'),
    });
  });

  it('normalises a payload that does not match the contract', async () => {
    stubApi(() => ({ status: 200, data: { data: { status: 'ok' } } }));

    await expect(healthApi.get()).rejects.toMatchObject({
      message: expect.stringContaining('tidak dikenali'),
    });
  });

  it('normalises an unknown failure category instead of leaking the zod error', async () => {
    const fixture = loadContract('health.degraded.response.json') as {
      data: { checks: Record<string, { error?: string }> };
    };
    fixture.data.checks.freeradius_mysql.error = 'dial tcp 172.18.0.5:3306: connection refused';

    stubApi(() => ({ status: 503, data: fixture }));

    await expect(healthApi.get()).rejects.toMatchObject({
      message: expect.stringContaining('tidak dikenali'),
    });
  });
});
