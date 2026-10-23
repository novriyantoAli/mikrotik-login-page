import { z } from 'zod';

/**
 * The checkout contract. It mirrors the Go DTOs in
 * `backend/internal/application/transaction/dto` exactly; the shared fixtures in
 * `contracts/` are what both sides are tested against, and both schemas are
 * strict so an unexpected field fails loudly instead of leaking through.
 */

export const TransactionStateSchema = z.enum([
  'pending',
  'paid',
  'provisioned',
  'failed',
  'expired',
]);
export type TransactionState = z.infer<typeof TransactionStateSchema>;

/** States the app stops polling in: nothing will change them on its own. */
const terminalStates: TransactionState[] = ['paid', 'provisioned', 'failed', 'expired'];

export function isTerminalState(state: TransactionState | undefined): boolean {
  return state !== undefined && terminalStates.includes(state);
}

export const ChannelSchema = z
  .object({
    code: z.string().min(1),
    name: z.string().min(1),
    group: z.string(),
    fee_flat_idr: z.number().int().nonnegative(),
    fee_percent: z.number().nonnegative(),
  })
  .strict();
export type Channel = z.infer<typeof ChannelSchema>;

export const ChannelListEnvelopeSchema = z.object({ data: z.array(ChannelSchema) }).strict();

export const InstructionSchema = z
  .object({
    title: z.string().min(1),
    steps: z.array(z.string()),
  })
  .strict();
export type Instruction = z.infer<typeof InstructionSchema>;

export const TransactionSchema = z
  .object({
    id: z.number().int().positive(),
    reference: z.string().min(1),
    package_id: z.number().int().positive(),
    package_name: z.string().min(1),
    amount_idr: z.number().int().positive(),
    channel: z.string().min(1),
    channel_name: z.string(),
    state: TransactionStateSchema,
    pay_code: z.string(),
    pay_url: z.string(),
    qr_string: z.string(),
    qr_url: z.string(),
    instructions: z.array(InstructionSchema),
    return_url: z.string(),
    expires_at: z.string().min(1),
    paid_at: z.string().nullable(),
    created_at: z.string().min(1),
  })
  .strict();
export type Transaction = z.infer<typeof TransactionSchema>;

export const TransactionEnvelopeSchema = z.object({ data: TransactionSchema }).strict();

/**
 * What the app sends to open (or re-open) a Transaction. The backend decides the
 * amount and validates `return_url` against the deployment allowlist; the app
 * never sends an amount.
 */
export const CreateTransactionInputSchema = z
  .object({
    package_id: z.number().int().positive(),
    channel: z.string().min(1),
    return_url: z.string().url().optional(),
  })
  .strict();
export type CreateTransactionInput = z.infer<typeof CreateTransactionInputSchema>;

/**
 * How often to re-read a Transaction. Polling stops at a terminal state and while
 * the tab is not visible — the callback is the authority, polling is only how the
 * buyer sees it happen (PRD point 28).
 */
export function pollingInterval(
  state: TransactionState | undefined,
  hidden: boolean,
): number | false {
  if (isTerminalState(state)) {
    return false;
  }
  if (hidden) {
    return false;
  }
  return 4000;
}
