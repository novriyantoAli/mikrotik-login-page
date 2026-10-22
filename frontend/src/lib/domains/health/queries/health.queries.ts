import { createQuery } from '@tanstack/svelte-query';

import { env } from '$lib/config/env';
import { healthApi } from '../api/health.api';

/** Query keys for this domain. Components never write literal keys. */
export const healthKeys = {
  all: ['health'] as const,
};

/** Live status of the backend and its dependencies, refreshed on an interval. */
export function createHealthQuery() {
  return createQuery(() => ({
    queryKey: healthKeys.all,
    queryFn: () => healthApi.get(),
    refetchInterval: env.healthRefreshIntervalMs,
    staleTime: 5_000,
  }));
}
