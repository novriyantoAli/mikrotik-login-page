import { apiClient, isAppError, type AppError } from '$lib/api/client';

import {
  AdminPackageEnvelopeSchema,
  AdminPackageListEnvelopeSchema,
  CreatePackageInputSchema,
  SetActiveInputSchema,
  UpdatePackageInputSchema,
  readVersionConflict,
  type AdminPackage,
  type CreatePackageInput,
  type SetActiveInput,
  type UpdatePackageInput,
  type VersionConflict,
} from '../schemas/package.schema';

/**
 * The catalog's panel HTTP surface. `queries/` depends on this interface, so a
 * test can swap the implementation without touching a component (skill §3).
 */
export interface PackageApi {
  list(): Promise<AdminPackage[]>;
  create(input: CreatePackageInput): Promise<AdminPackage>;
  update(id: number, input: UpdatePackageInput): Promise<AdminPackage>;
  setActive(id: number, input: SetActiveInput): Promise<AdminPackage>;
}

/** A stale edit, recognised from the 409 body the service sends (ADR 0025 §7). */
export type PackageVersionConflict = VersionConflict['error'];

/**
 * Whether a failed edit is a version conflict, and what the row looks like now.
 * Components branch on this to show the current values and a reload path
 * instead of a bare "conflict".
 */
export function versionConflictOf(error: unknown): PackageVersionConflict | null {
  if (!isAppError(error)) {
    return null;
  }
  const appError: AppError = error;
  if (appError.status !== 409) {
    return null;
  }
  return readVersionConflict(appError.data);
}

export const packageApi: PackageApi = {
  async list(): Promise<AdminPackage[]> {
    const { data } = await apiClient.get('/admin/packages');
    return AdminPackageListEnvelopeSchema.parse(data).data;
  },

  async create(input: CreatePackageInput): Promise<AdminPackage> {
    const payload = CreatePackageInputSchema.parse(input);
    const { data } = await apiClient.post('/admin/packages', payload);
    return AdminPackageEnvelopeSchema.parse(data).data;
  },

  async update(id: number, input: UpdatePackageInput): Promise<AdminPackage> {
    const payload = UpdatePackageInputSchema.parse(input);
    const { data } = await apiClient.put(`/admin/packages/${id}`, payload);
    return AdminPackageEnvelopeSchema.parse(data).data;
  },

  async setActive(id: number, input: SetActiveInput): Promise<AdminPackage> {
    const payload = SetActiveInputSchema.parse(input);
    const { data } = await apiClient.post(`/admin/packages/${id}/active`, payload);
    return AdminPackageEnvelopeSchema.parse(data).data;
  },
};
