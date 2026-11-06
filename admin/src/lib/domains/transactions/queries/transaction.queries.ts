import { createQuery } from '@tanstack/svelte-query';

import { transactionApi } from '../api/transaction.api';
import type { AdminTransactionFilter } from '../schemas/transaction.schema';

/** Query keys for this domain. Components never write literal keys. */
export const transactionKeys = {
  all: ['admin-transactions'] as const,
  list: (filter: AdminTransactionFilter) => [...transactionKeys.all, 'list', filter] as const,
};

/**
 * One page of the panel's Transaction search. The filter is a thunk so the query
 * re-derives when the operator changes it (see the domain's filter state); the
 * whole filter is part of the key, so two searches never share a cached page.
 */
export function createAdminTransactionListQuery(filter: () => AdminTransactionFilter) {
  return createQuery(() => {
    const current = filter();
    return {
      queryKey: transactionKeys.list(current),
      queryFn: () => transactionApi.list(current),
      // A search answers a support call: a page that is a few seconds stale is
      // still an answer, and refetching on every keystroke is not.
      staleTime: 15_000,
    };
  });
}
