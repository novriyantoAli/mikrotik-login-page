import { apiClient } from '$lib/api/client';

import {
  ChannelListEnvelopeSchema,
  CreateTransactionInputSchema,
  TransactionEnvelopeSchema,
  type Channel,
  type CreateTransactionInput,
  type Transaction,
} from '../schemas/checkout.schema';

/**
 * The checkout's HTTP surface. `queries/` depends on this interface, so a test or
 * a story can provide a fake without touching a component.
 */
export interface CheckoutApi {
  channels(): Promise<Channel[]>;
  /** Open the pending Transaction for a Package, or return the one that exists. */
  create(input: CreateTransactionInput): Promise<Transaction>;
  get(id: number): Promise<Transaction>;
}

export const checkoutApi: CheckoutApi = {
  async channels(): Promise<Channel[]> {
    const { data } = await apiClient.get('/transactions/channels');
    return ChannelListEnvelopeSchema.parse(data).data;
  },

  async create(input: CreateTransactionInput): Promise<Transaction> {
    const payload = CreateTransactionInputSchema.parse(input);
    const { data } = await apiClient.post('/transactions', payload);
    return TransactionEnvelopeSchema.parse(data).data;
  },

  async get(id: number): Promise<Transaction> {
    const { data } = await apiClient.get(`/transactions/${id}`);
    return TransactionEnvelopeSchema.parse(data).data;
  },
};
