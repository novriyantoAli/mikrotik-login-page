import { apiClient } from '$lib/api/client';

import {
  AccountEnvelopeSchema,
  ExtendPreviewEnvelopeSchema,
  TransactionHistorySchema,
  type Account,
  type ExtendPreview,
  type TransactionHistory,
} from '../schemas/account.schema';

/**
 * The Account Status HTTP surface. `queries/` depends on this interface, so a
 * test or a story can provide a fake without touching a component.
 */
export interface AccountApi {
  /** The session's summary: Active Period, active Package and current PIN. */
  summary(): Promise<Account>;
  /** One page of the session's Transaction history, newest first, without PINs. */
  history(page: number, pageSize: number): Promise<TransactionHistory>;
  /** Where buying `packageId` would leave the Active Period. */
  extendPreview(packageId: number): Promise<ExtendPreview>;
}

export const accountApi: AccountApi = {
  async summary(): Promise<Account> {
    const { data } = await apiClient.get('/account');
    return AccountEnvelopeSchema.parse(data).data;
  },

  async history(page: number, pageSize: number): Promise<TransactionHistory> {
    const { data } = await apiClient.get('/transactions', {
      params: { page, page_size: pageSize },
    });
    return TransactionHistorySchema.parse(data);
  },

  async extendPreview(packageId: number): Promise<ExtendPreview> {
    const { data } = await apiClient.get('/account/extend-preview', {
      params: { package_id: packageId },
    });
    return ExtendPreviewEnvelopeSchema.parse(data).data;
  },
};
