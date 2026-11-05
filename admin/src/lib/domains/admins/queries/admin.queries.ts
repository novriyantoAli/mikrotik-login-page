import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';

import { adminApi } from '../api/admin.api';
import type { CreateAdminInput, ResetPasswordInput, SetActiveInput } from '../schemas/admin.schema';

/** Query keys for this domain. Components never write literal keys. */
export const adminKeys = {
  all: ['admin-admins'] as const,
  list: () => [...adminKeys.all, 'list'] as const,
};

/** Every admin of the panel, active or not. Owner only on the server. */
export function createAdminListQuery() {
  return createQuery(() => ({
    queryKey: adminKeys.list(),
    queryFn: () => adminApi.list(),
    staleTime: 30_000,
  }));
}

export function createAdminMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: (input: CreateAdminInput) => adminApi.create(input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminKeys.all }),
  }));
}

export function createResetAdminPasswordMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: (variables: { id: number; input: ResetPasswordInput }) =>
      adminApi.resetPassword(variables.id, variables.input),
    // A reset revokes that admin's sessions, never this one; the list itself is
    // unchanged, so only a stale read would be misleading.
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminKeys.all }),
  }));
}

export function createSetAdminActiveMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: (variables: { id: number; input: SetActiveInput }) =>
      adminApi.setActive(variables.id, variables.input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminKeys.all }),
  }));
}
