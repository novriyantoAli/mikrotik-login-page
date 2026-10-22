import { describe, expect, it } from 'vitest';

import { HealthEnvelopeSchema } from '$lib/domains/health';
import { loadContract } from '../support/contracts';

interface HealthFixture {
  data: {
    status?: string;
    checks: Record<string, { status?: string; latency_ms?: number; error?: string }>;
  };
}

describe('health contract fixture', () => {
  it('parses the shared healthy fixture with the strict schema', () => {
    const parsed = HealthEnvelopeSchema.parse(loadContract('health.response.json'));

    expect(parsed.data.status).toBe('ok');
    expect(Object.keys(parsed.data.checks)).toEqual(['postgres', 'freeradius_mysql', 'redis']);
    expect(parsed.data.checks.postgres.status).toBe('up');
  });

  it('parses the shared degraded fixture, including the failure category', () => {
    const parsed = HealthEnvelopeSchema.parse(loadContract('health.degraded.response.json'));

    expect(parsed.data.status).toBe('degraded');
    expect(parsed.data.checks.freeradius_mysql).toEqual({
      status: 'down',
      latency_ms: 12,
      error: 'unreachable',
    });
  });

  it('fails when a field is removed from the fixture', () => {
    const fixture = structuredClone(loadContract<HealthFixture>('health.response.json'));
    delete fixture.data.checks.redis.latency_ms;

    expect(HealthEnvelopeSchema.safeParse(fixture).success).toBe(false);
  });

  it('fails when the fixture grows a field the app does not model', () => {
    const fixture = structuredClone(loadContract<HealthFixture>('health.response.json'));
    (fixture.data.checks.redis as Record<string, unknown>).cpu_percent = 12;

    expect(HealthEnvelopeSchema.safeParse(fixture).success).toBe(false);
  });

  it('fails when a down dependency loses its failure category', () => {
    const fixture = structuredClone(loadContract<HealthFixture>('health.degraded.response.json'));
    delete fixture.data.checks.freeradius_mysql.error;

    expect(HealthEnvelopeSchema.safeParse(fixture).success).toBe(false);
  });

  it('fails when a failure category is invented', () => {
    const fixture = structuredClone(loadContract<HealthFixture>('health.degraded.response.json'));
    fixture.data.checks.freeradius_mysql.error = 'dial tcp 172.18.0.5:3306: connection refused';

    expect(HealthEnvelopeSchema.safeParse(fixture).success).toBe(false);
  });
});
