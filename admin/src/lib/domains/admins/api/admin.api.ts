import { apiClient } from '$lib/api/client';

import {
  AdminEnvelopeSchema,
  AdminListEnvelopeSchema,
  CreateAdminInputSchema,
  ResetPasswordInputSchema,
  SetActiveEnvelopeSchema,
  SetActiveInputSchema,
  type Admin,
  type CreateAdminInput,
  type ResetPasswordInput,
  type SetActiveInput,
  type SetActiveResponse,
} from '../schemas/admin.schema';

/**
 * The admin-management HTTP surface. Every call is owner-only on the server;
 * `queries/` depends on this interface so a test can swap it out.
 */
export interface AdminApi {
  list(): Promise<Admin[]>;
  create(input: CreateAdminInput): Promise<Admin>;
  resetPassword(id: number, input: ResetPasswordInput): Promise<void>;
  setActive(id: number, input: SetActiveInput): Promise<SetActiveResponse>;
}

export const adminApi: AdminApi = {
  async list(): Promise<Admin[]> {
    const { data } = await apiClient.get('/admin/admins');
    return AdminListEnvelopeSchema.parse(data).data;
  },

  async create(input: CreateAdminInput): Promise<Admin> {
    const payload = CreateAdminInputSchema.parse(input);
    const { data } = await apiClient.post('/admin/admins', payload);
    return AdminEnvelopeSchema.parse(data).data;
  },

  async resetPassword(id: number, input: ResetPasswordInput): Promise<void> {
    const payload = ResetPasswordInputSchema.parse(input);
    await apiClient.post(`/admin/admins/${id}/password`, payload);
  },

  async setActive(id: number, input: SetActiveInput): Promise<SetActiveResponse> {
    const payload = SetActiveInputSchema.parse(input);
    const { data } = await apiClient.post(`/admin/admins/${id}/active`, payload);
    return SetActiveEnvelopeSchema.parse(data).data;
  },
};
