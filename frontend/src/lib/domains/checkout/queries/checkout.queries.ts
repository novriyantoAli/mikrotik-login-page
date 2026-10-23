import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';

import { checkoutApi } from '../api/checkout.api';
import {
  pollingInterval,
  type CreateTransactionInput,
  type Transaction,
} from '../schemas/checkout.schema';

/** Query keys for this domain. Components never write literal keys. */
export const checkoutKeys = {
  all: ['checkout'] as const,
  channels: () => [...checkoutKeys.all, 'channels'] as const,
  transaction: (id: number) => [...checkoutKeys.all, 'transaction', id] as const,
};

/** The closed-payment channels the buyer may pick, with their fees. */
export function createChannelsQuery() {
  return createQuery(() => ({
    queryKey: checkoutKeys.channels(),
    queryFn: () => checkoutApi.channels(),
    staleTime: 5 * 60_000,
  }));
}

/** Opens the pending Transaction (or returns the existing one) for a Package. */
export function createCreateTransactionMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: (input: CreateTransactionInput) => checkoutApi.create(input),
    onSuccess: (transaction: Transaction) => {
      // Seed the detail cache so the instructions render without a second call.
      queryClient.setQueryData(checkoutKeys.transaction(transaction.id), transaction);
    },
  }));
}

/**
 * One Transaction, polled until it settles. `id` is a thunk so the query follows
 * the runes state that holds the current Transaction.
 */
export function createTransactionQuery(id: () => number | null) {
  return createQuery(() => {
    const transactionId = id();

    return {
      queryKey: checkoutKeys.transaction(transactionId ?? 0),
      queryFn: () => checkoutApi.get(transactionId as number),
      enabled: transactionId !== null,
      staleTime: 0,
      refetchOnWindowFocus: true,
      refetchIntervalInBackground: false,
      refetchInterval: (query: { state: { data?: Transaction } }) =>
        pollingInterval(
          query.state.data?.state,
          typeof document !== 'undefined' && document.hidden,
        ),
    };
  });
}
