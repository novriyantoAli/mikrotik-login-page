import { createQuery } from '@tanstack/svelte-query';

import { packageApi } from '../api/package.api';

/** Query keys for this domain. Components never write literal keys. */
export const packageKeys = {
  all: ['packages'] as const,
  list: () => [...packageKeys.all, 'list'] as const,
};

/**
 * The public Package catalog. It changes only when an operator edits it, so a
 * longer stale time is enough; checkout reads its price from the backend anyway.
 */
export function createPackageListQuery() {
  return createQuery(() => ({
    queryKey: packageKeys.list(),
    queryFn: () => packageApi.list(),
    staleTime: 60_000,
  }));
}
