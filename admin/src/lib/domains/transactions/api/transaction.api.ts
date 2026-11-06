import { AppError, apiClient } from '$lib/api/client';

import {
  AdminTransactionFilterSchema,
  AdminTransactionListResponseSchema,
  toQueryParams,
  type AdminTransactionFilter,
  type AdminTransactionListResponse,
} from '../schemas/transaction.schema';

/**
 * The panel's Transaction search HTTP surface. `queries/` depends on this
 * interface, so a test can swap the implementation without touching a component
 * (skill §3). Every response is parsed through the schema before it returns: an
 * unexpected shape fails here, not inside a table cell.
 */
export interface TransactionApi {
  list(filter: AdminTransactionFilter): Promise<AdminTransactionListResponse>;
}

export const transactionApi: TransactionApi = {
  async list(filter: AdminTransactionFilter): Promise<AdminTransactionListResponse> {
    const parsed = AdminTransactionFilterSchema.parse(filter);
    const { data } = await apiClient.get('/admin/transactions', {
      params: toQueryParams(parsed),
    });

    const response = AdminTransactionListResponseSchema.safeParse(data);
    if (!response.success) {
      // The strict schema is this list's defence — an unexpected field (a PIN, for
      // one) must fail the read, not reach a table cell. A raw zod issue list is
      // not operator copy, so the panel shows one sentence instead.
      throw new AppError('Respons server tidak sesuai dengan kontrak panel.');
    }
    return response.data;
  },
};
