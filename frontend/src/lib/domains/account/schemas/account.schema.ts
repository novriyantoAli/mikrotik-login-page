import { z } from 'zod';

/**
 * The Account Status contract (ADR 0017). Field names match the Go DTOs in
 * `backend/internal/application/account/dto` exactly; the shared fixtures in
 * `contracts/` are what both sides are tested against, and both schemas are
 * strict so an unexpected field fails loudly.
 */

/**
 * What the backend already decided about the Active Period. The app renders this
 * value and never compares its own clock with `expires_at`, so the Login Page and
 * Account Status cannot disagree about "almost over" (ADR 0017).
 */
export const ActivePeriodStateSchema = z.enum(['active', 'expiring', 'expired', 'none']);
export type ActivePeriodState = z.infer<typeof ActivePeriodStateSchema>;

export const ActivePeriodSchema = z
  .object({
    expires_at: z.string().min(1).nullable(),
    found: z.boolean(),
    state: ActivePeriodStateSchema,
  })
  .strict();
export type ActivePeriod = z.infer<typeof ActivePeriodSchema>;

export const AccountPackageSchema = z
  .object({
    id: z.number().int().positive(),
    name: z.string().min(1),
    rate_limit: z.string().min(1),
  })
  .strict();
export type AccountPackage = z.infer<typeof AccountPackageSchema>;

/** The summary. `pin` is the only place a credential is returned from the API. */
export const AccountSchema = z
  .object({
    phone_number: z.string().min(1),
    active_period: ActivePeriodSchema,
    package: AccountPackageSchema.nullable(),
    pin: z.string().length(6).nullable(),
  })
  .strict();
export type Account = z.infer<typeof AccountSchema>;

export const AccountEnvelopeSchema = z.object({ data: AccountSchema }).strict();

/**
 * The Transaction states the history renders. They mirror the backend's state
 * machine; the account domain keeps its own copy so it does not depend on the
 * checkout domain (and checkout can depend on account without a cycle).
 */
export const HistoryStateSchema = z.enum(['pending', 'paid', 'provisioned', 'failed', 'expired']);
export type HistoryState = z.infer<typeof HistoryStateSchema>;

/** One history row: date, Package, amount and state — never a PIN. */
export const TransactionHistoryItemSchema = z
  .object({
    id: z.number().int().positive(),
    reference: z.string().min(1),
    package_id: z.number().int().positive(),
    package_name: z.string().min(1),
    amount_idr: z.number().int().positive(),
    channel: z.string(),
    channel_name: z.string(),
    state: HistoryStateSchema,
    created_at: z.string().min(1),
    paid_at: z.string().min(1).nullable(),
  })
  .strict();
export type TransactionHistoryItem = z.infer<typeof TransactionHistoryItemSchema>;

export const TransactionHistorySchema = z
  .object({
    data: z.array(TransactionHistoryItemSchema),
    total_count: z.number().int().nonnegative(),
    page: z.number().int().positive(),
    page_size: z.number().int().positive(),
  })
  .strict();
export type TransactionHistory = z.infer<typeof TransactionHistorySchema>;

/** The Extend estimate. It is a preview; provisioning recomputes the final value. */
export const ExtendPreviewSchema = z
  .object({
    package_id: z.number().int().positive(),
    current_expiration: z.string().min(1).nullable(),
    estimated_expiration: z.string().min(1),
    extends: z.boolean(),
  })
  .strict();
export type ExtendPreview = z.infer<typeof ExtendPreviewSchema>;

export const ExtendPreviewEnvelopeSchema = z.object({ data: ExtendPreviewSchema }).strict();
