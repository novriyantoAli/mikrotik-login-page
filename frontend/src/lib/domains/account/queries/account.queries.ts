import { createQuery } from '@tanstack/svelte-query';

import { accountApi } from '../api/account.api';

/** Query keys for this domain. Components never write literal keys. */
export const accountKeys = {
  all: ['account'] as const,
  summary: () => [...accountKeys.all, 'summary'] as const,
  history: (page: number, pageSize: number) =>
    [...accountKeys.all, 'history', { page, pageSize }] as const,
  extendPreview: (packageId: number) => [...accountKeys.all, 'extend-preview', packageId] as const,
};

/**
 * The Account Status summary. `enabled` is a thunk because the page must not ask
 * for an account before it knows there is a session — the answer would be a 401,
 * not data.
 */
export function createAccountSummaryQuery(enabled: () => boolean = () => true) {
  return createQuery(() => ({
    queryKey: accountKeys.summary(),
    queryFn: () => accountApi.summary(),
    enabled: enabled(),
    staleTime: 0,
  }));
}

/** One page of Transaction history; the thunk keeps it reactive to page state. */
export function createTransactionHistoryQuery(page: () => number, pageSize = 5) {
  return createQuery(() => ({
    queryKey: accountKeys.history(page(), pageSize),
    queryFn: () => accountApi.history(page(), pageSize),
    staleTime: 30_000,
  }));
}

/**
 * The Extend estimate for one Package. The buyer sees it before paying, so it is
 * only requested when it is actually shown (`enabled`).
 */
export function createExtendPreviewQuery(
  packageId: () => number,
  enabled: () => boolean = () => true,
) {
  return createQuery(() => ({
    queryKey: accountKeys.extendPreview(packageId()),
    queryFn: () => accountApi.extendPreview(packageId()),
    enabled: enabled(),
    staleTime: 0,
  }));
}
