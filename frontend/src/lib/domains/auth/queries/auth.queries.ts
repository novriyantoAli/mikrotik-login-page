import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';

import { authApi } from '../api/auth.api';
import type { MagicLinkRequest } from '../schemas/auth.schema';

/** Query keys for this domain. Components never write literal keys. */
export const authKeys = {
  all: ['auth'] as const,
  session: () => [...authKeys.all, 'session'] as const,
};

/**
 * The live session, or null when the browser has no usable cookie. The Account
 * Status screen and the checkout both branch on this.
 */
export function createSessionQuery() {
  return createQuery(() => ({
    queryKey: authKeys.session(),
    queryFn: () => authApi.session(),
    staleTime: 30_000,
    retry: false,
  }));
}

/** Ask for a Magic Link. Success only means the SMS is queued, never that it arrived. */
export function createRequestMagicLinkMutation() {
  return createMutation(() => ({
    mutationFn: (input: MagicLinkRequest) => authApi.requestMagicLink(input),
  }));
}

/** Redeem the token from the SMS. Success opens the session cookie and invalidates it. */
export function createExchangeMagicLinkMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: (token: string) => authApi.exchange(token),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: authKeys.all }),
  }));
}

/** Revoke the session server-side, then refresh the session query. */
export function createSignOutMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: () => authApi.signOut(),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: authKeys.all }),
  }));
}
