import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';

import { packageApi } from '../api/package.api';
import type {
  CreatePackageInput,
  SetActiveInput,
  UpdatePackageInput,
} from '../schemas/package.schema';

/** Query keys for this domain. Components never write literal keys. */
export const packageKeys = {
  all: ['admin-packages'] as const,
  list: () => [...packageKeys.all, 'list'] as const,
};

/** Every Package, retired ones included: the panel decides, the Login Page filters. */
export function createAdminPackageListQuery() {
  return createQuery(() => ({
    queryKey: packageKeys.list(),
    queryFn: () => packageApi.list(),
    staleTime: 30_000,
  }));
}

export function createPackageMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: (input: CreatePackageInput) => packageApi.create(input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: packageKeys.all }),
  }));
}

export function createUpdatePackageMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: (variables: { id: number; input: UpdatePackageInput }) =>
      packageApi.update(variables.id, variables.input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: packageKeys.all }),
  }));
}

export function createSetActivePackageMutation() {
  const queryClient = useQueryClient();

  return createMutation(() => ({
    mutationFn: (variables: { id: number; input: SetActiveInput }) =>
      packageApi.setActive(variables.id, variables.input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: packageKeys.all }),
  }));
}
