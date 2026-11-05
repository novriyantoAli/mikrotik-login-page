import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';

import { authApi } from '../api/auth.api';
import type { ChangePasswordInput, LoginInput, Session } from '../schemas/auth.schema';

/** Query keys for this domain. Components never write literal keys. */
export const authKeys = {
  all: ['admin-auth'] as const,
  session: () => [...authKeys.all, 'session'] as const,
};

/**
 * The live admin session, or null when the browser has no usable cookie. The
 * shell branches on this: null means the login form, never a blank page.
 */
export function createSessionQuery() {
  return createQuery(() => ({
    queryKey: authKeys.session(),
    queryFn: () => authApi.session(),
    staleTime: 30_000,
    retry: false,
  }));
}

/** Sign in. Success writes the session straight into the cache, no second read. */
export function createLoginMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: (input: LoginInput) => authApi.login(input),
    onSuccess: (session: Session) => {
      queryClient.setQueryData(authKeys.session(), session);
    },
  }));
}

/** Revoke the session server-side, then forget it locally. */
export function createLogoutMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: () => authApi.logout(),
    onSuccess: () => {
      queryClient.setQueryData(authKeys.session(), null);
    },
  }));
}

/** Replace the signed-in admin's own password. The session stays valid. */
export function createChangePasswordMutation() {
  return createMutation(() => ({
    mutationFn: (input: ChangePasswordInput) => authApi.changePassword(input),
  }));
}
