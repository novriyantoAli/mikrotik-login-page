import { AppError, apiClient } from '$lib/api/client';

import {
  AdminAuditFilterSchema,
  AdminAuditListResponseSchema,
  toAuditQueryParams,
  type AdminAuditFilter,
  type AdminAuditListResponse,
} from '../schemas/audit.schema';

/**
 * The panel's audit reader is one read: there is no create, update or delete to
 * expose, because the trail is append-only (ADR 0027 §3). `queries/` depends on this
 * interface, so a test can swap the implementation without touching a component
 * (skill §3).
 */
export interface AuditApi {
  list(filter: AdminAuditFilter): Promise<AdminAuditListResponse>;
}

export const auditApi: AuditApi = {
  async list(filter: AdminAuditFilter): Promise<AdminAuditListResponse> {
    const parsed = AdminAuditFilterSchema.parse(filter);
    const { data } = await apiClient.get('/admin/audit', {
      params: toAuditQueryParams(parsed),
    });

    const response = AdminAuditListResponseSchema.safeParse(data);
    if (!response.success) {
      // The strict schema is this list's defence: an unexpected column fails the
      // read instead of reaching a table cell. A raw zod issue list is not auditor
      // copy, so the panel shows one sentence instead.
      throw new AppError('Respons server tidak sesuai dengan kontrak audit panel.');
    }
    return response.data;
  },
};
