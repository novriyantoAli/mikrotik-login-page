import { createQuery } from '@tanstack/svelte-query';

import { auditApi } from '../api/audit.api';
import type { AdminAuditFilter } from '../schemas/audit.schema';

/** Query keys for this domain. Components never write literal keys. */
export const auditKeys = {
  all: ['admin-audit'] as const,
  list: (filter: AdminAuditFilter) => [...auditKeys.all, 'list', filter] as const,
};

/**
 * One page of the trail. The filter is a thunk so the query re-derives when the
 * auditor changes it (see the domain's filter state); the whole filter is part of
 * the key, so two searches never share a cached page.
 *
 * There is no mutation in this domain: the reader cannot change or remove a row.
 */
export function createAuditListQuery(filter: () => AdminAuditFilter) {
  return createQuery(() => {
    const current = filter();
    return {
      queryKey: auditKeys.list(current),
      queryFn: () => auditApi.list(current),
      // A trail row is written, never edited, so a page a few seconds old is still
      // an accurate answer.
      staleTime: 15_000,
    };
  });
}
