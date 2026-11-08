import { z } from 'zod';

import { formatPriceIDR } from '$lib/utils';

/**
 * The panel's Transaction search contract (PRD #35). Field names mirror
 * `backend/internal/application/transaction/dto/transaction.dto.go`, and the
 * shared fixture `contracts/admin-transactions.response.json` is what both sides
 * are tested against.
 *
 * There is deliberately no `pin` field here, and the schema is strict: a list
 * response that grows one fails to parse instead of leaking a credential into a
 * screen nobody asked it for. Opening a PIN is its own audited endpoint.
 */

/** The five states the Transaction lifecycle knows. */
export const TRANSACTION_STATES = ['pending', 'paid', 'provisioned', 'failed', 'expired'] as const;
export const TransactionStateSchema = z.enum(TRANSACTION_STATES);
export type TransactionState = z.infer<typeof TransactionStateSchema>;

/** The four states one `sms_outbox` row can hold. */
export const SMS_STATES = ['pending', 'sending', 'sent', 'failed'] as const;
export const SmsStateSchema = z.enum(SMS_STATES);
export type SmsState = z.infer<typeof SmsStateSchema>;

/** One row of GET /api/admin/transactions. */
export const AdminTransactionSchema = z
  .object({
    id: z.number().int().positive(),
    reference: z.string().min(1),
    phone_number: z.string().min(1),
    package_id: z.number().int().positive(),
    // The terms the buyer paid for, frozen at checkout (ADR 0026): never the
    // current catalog row.
    package_name: z.string().min(1),
    amount_idr: z.number().int().nonnegative(),
    channel: z.string().min(1),
    channel_name: z.string(),
    state: TransactionStateSchema,
    gateway_reference: z.string(),
    provision_attempts: z.number().int().nonnegative(),
    // The newest outbox row for this Phone Number, or null when nothing was queued.
    last_sms_state: SmsStateSchema.nullable(),
    last_sms_at: z.string().nullable(),
    created_at: z.string().min(1),
    paid_at: z.string().nullable(),
    expires_at: z.string().min(1),
  })
  .strict();
export type AdminTransaction = z.infer<typeof AdminTransactionSchema>;

/** The shared pagination envelope every list in the panel answers with. */
export const AdminTransactionListResponseSchema = z
  .object({
    data: z.array(AdminTransactionSchema),
    total_count: z.number().int().nonnegative(),
    page: z.number().int().positive(),
    page_size: z.number().int().positive(),
  })
  .strict();
export type AdminTransactionListResponse = z.infer<typeof AdminTransactionListResponseSchema>;

/**
 * The panel's one credential: `POST /api/admin/transactions/{id}/pin` answers the
 * six digits inside the shared envelope, and nothing else (ADR 0027 §4). The schema
 * is strict and the digits are checked, so a response that grows a field, or a value
 * that is not a PIN, fails here instead of reaching a screen.
 */
export const PIN_LENGTH = 6;

export const AdminPINRevealResponseSchema = z
  .object({
    data: z
      .object({
        pin: z.string().regex(new RegExp(`^\\d{${PIN_LENGTH}}$`)),
      })
      .strict(),
  })
  .strict();
export type AdminPINRevealResponse = z.infer<typeof AdminPINRevealResponseSchema>;

export const MAX_TRANSACTION_PAGE_SIZE = 100;
export const DEFAULT_TRANSACTION_PAGE_SIZE = 20;

/**
 * The search an operator runs. `state` and the dates are optional: an empty
 * filter is "everything, newest first". Dates are `YYYY-MM-DD` days, as the
 * backend's search expects — `from` and `to` are both included.
 */
export const AdminTransactionFilterSchema = z.object({
  state: TransactionStateSchema.optional(),
  phone_number: z.string().trim().optional(),
  package_id: z.number().int().positive().optional(),
  from: z.string().trim().optional(),
  to: z.string().trim().optional(),
  page: z.number().int().positive().default(1),
  page_size: z
    .number()
    .int()
    .positive()
    .max(MAX_TRANSACTION_PAGE_SIZE)
    .default(DEFAULT_TRANSACTION_PAGE_SIZE),
});
export type AdminTransactionFilter = z.infer<typeof AdminTransactionFilterSchema>;

/** The filter the panel sends as a query string: empty values are left out. */
export function toQueryParams(filter: AdminTransactionFilter): Record<string, string | number> {
  const params: Record<string, string | number> = {
    page: filter.page,
    page_size: filter.page_size,
  };
  if (filter.state) {
    params.state = filter.state;
  }
  if (filter.phone_number) {
    params.phone_number = filter.phone_number;
  }
  if (filter.package_id) {
    params.package_id = filter.package_id;
  }
  if (filter.from) {
    params.from = filter.from;
  }
  if (filter.to) {
    params.to = filter.to;
  }
  return params;
}

/** Human copy for the state the operator reads next to a Transaction. */
export const transactionStateLabels: Record<TransactionState, string> = {
  pending: 'Menunggu bayar',
  paid: 'Lunas, belum provisioning',
  provisioned: 'Aktif (provisioned)',
  failed: 'Gagal',
  expired: 'Kedaluwarsa',
};

/** Human copy for the last SMS outcome, the panel's "did the PIN arrive?". */
export const smsStateLabels: Record<SmsState, string> = {
  pending: 'Tertunda',
  sending: 'Dikirim',
  sent: 'Terkirim',
  failed: 'Gagal',
};

export function formatPrice(amountIDR: number): string {
  return formatPriceIDR(amountIDR);
}

/**
 * The clock the panel searches and shows in. Day filters are parsed in this same
 * zone on the backend (`panelDayLocation`, WIB), so the column an operator reads
 * and the day they filter on can never disagree.
 */
export const PANEL_TIME_ZONE = 'Asia/Jakarta';

/** Render an RFC 3339 instant in WIB, or an em dash when absent. */
export function formatDateTime(value: string | null): string {
  if (!value) {
    return '—';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat('id-ID', {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: PANEL_TIME_ZONE,
  }).format(parsed);
}
