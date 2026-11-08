import { z } from 'zod';

/**
 * The panel's audit-reader contract (PRD #38, ADR 0027 §3). Field names mirror
 * `backend/internal/application/admin/dto/audit.dto.go`, and the shared fixture
 * `contracts/admin-audit.response.json` is what both sides are tested against.
 *
 * The envelope and every column are strict; the payload is not. A trail row's
 * payload is whatever the writer recorded — a create carries `after`, an edit
 * carries `before`/`after` per changed field, a reveal carries its outcome — so
 * modelling one shape per action would make the reader break the day an action
 * gains a field. The table is append-only, and nothing here can alter a row.
 */

/**
 * Every action the backend's `pkg/audit` vocabulary can write. The backend is the
 * authority: a row with an action this build does not list still renders (the column
 * is a string), it just cannot be chosen in the filter until the panel ships an
 * update — the cost of releasing the panel independently (ADR 0028).
 */
export const AUDIT_ACTIONS = [
  'package.create',
  'package.update',
  'package.activate',
  'package.deactivate',
  'pin.reveal',
  'admin.create',
  'admin.update',
  'admin.password_reset',
  'admin.deactivate',
] as const;
export const AuditActionSchema = z.enum(AUDIT_ACTIONS);
export type AuditAction = z.infer<typeof AuditActionSchema>;

/**
 * One row of GET /api/admin/audit. `admin_email` is copied onto the row at write
 * time, so it still reads after the admin it names is gone; `target_id` is text and
 * may point at a row that no longer exists (a reveal attempted on a missing
 * Transaction), which the reader shows as-is.
 */
export const AdminAuditEntrySchema = z
  .object({
    id: z.number().int().positive(),
    created_at: z.string().min(1),
    admin_email: z.string().min(1),
    // A string rather than the enum: an action the panel's build does not know yet
    // must render as itself, not fail the whole page. The filter below is stricter,
    // because only the actions this build offers can be sent.
    action: z.string().min(1),
    target_type: z.string(),
    target_id: z.string(),
    payload: z.record(z.string(), z.unknown()),
    ip: z.string(),
  })
  .strict();
export type AdminAuditEntry = z.infer<typeof AdminAuditEntrySchema>;

/** The shared pagination envelope every list in the panel answers with. */
export const AdminAuditListResponseSchema = z
  .object({
    data: z.array(AdminAuditEntrySchema),
    total_count: z.number().int().nonnegative(),
    page: z.number().int().positive(),
    page_size: z.number().int().positive(),
  })
  .strict();
export type AdminAuditListResponse = z.infer<typeof AdminAuditListResponseSchema>;

export const MAX_AUDIT_PAGE_SIZE = 100;
export const DEFAULT_AUDIT_PAGE_SIZE = 20;

/**
 * The search an auditor runs. An empty filter is "the whole trail, newest first";
 * dates are `YYYY-MM-DD` days, as the backend's WIB day window expects.
 */
export const AdminAuditFilterSchema = z.object({
  admin_email: z.string().trim().optional(),
  action: AuditActionSchema.optional(),
  from: z.string().trim().optional(),
  to: z.string().trim().optional(),
  page: z.number().int().positive().default(1),
  page_size: z.number().int().positive().max(MAX_AUDIT_PAGE_SIZE).default(DEFAULT_AUDIT_PAGE_SIZE),
});
export type AdminAuditFilter = z.infer<typeof AdminAuditFilterSchema>;

/** The filter the panel sends as a query string: empty values are left out. */
export function toAuditQueryParams(filter: AdminAuditFilter): Record<string, string | number> {
  const params: Record<string, string | number> = {
    page: filter.page,
    page_size: filter.page_size,
  };
  if (filter.admin_email) {
    params.admin_email = filter.admin_email;
  }
  if (filter.action) {
    params.action = filter.action;
  }
  if (filter.from) {
    params.from = filter.from;
  }
  if (filter.to) {
    params.to = filter.to;
  }
  return params;
}

/** Human copy for each action, so a table cell reads as an act, not a code. */
export const auditActionLabels: Record<AuditAction, string> = {
  'package.create': 'Buat Package',
  'package.update': 'Ubah Package',
  'package.activate': 'Aktifkan Package',
  'package.deactivate': 'Nonaktifkan Package',
  'pin.reveal': 'Buka PIN',
  'admin.create': 'Tambah admin',
  'admin.update': 'Ubah admin',
  'admin.password_reset': 'Reset kata sandi',
  'admin.deactivate': 'Nonaktifkan admin',
};

/** Human copy for a target, tolerant of a value this build does not know. */
export function targetTypeLabel(targetType: string): string {
  switch (targetType) {
    case 'package':
      return 'Package';
    case 'transaction':
      return 'Transaksi';
    case 'admin':
      return 'Admin';
    default:
      return targetType;
  }
}

/** Human copy for an action, tolerant of a value this build does not know. */
export function auditActionLabel(action: string): string {
  return (auditActionLabels as Record<string, string>)[action] ?? action;
}

/** Human copy for the payload keys the writers actually record. */
export const auditFieldLabels: Record<string, string> = {
  before: 'Sebelum',
  after: 'Sesudah',
  code: 'Code',
  name: 'Nama',
  price_idr: 'Harga',
  duration_minutes: 'Durasi (menit)',
  rate_limit: 'Rate limit',
  is_active: 'Aktif',
  version: 'Versi',
  email: 'Email',
  role: 'Peran',
  result: 'Hasil',
  reference: 'Reference',
};

export function auditFieldLabel(field: string): string {
  return auditFieldLabels[field] ?? field;
}

/** One rendered field of a payload: either `value`, or `before → after`. */
export interface AuditPayloadField {
  key: string;
  label: string;
  before?: unknown;
  after?: unknown;
  value?: unknown;
  /** True when the writer recorded the field as a before/after change. */
  change: boolean;
}

function isChange(value: unknown): value is { before: unknown; after: unknown } {
  return (
    typeof value === 'object' &&
    value !== null &&
    !Array.isArray(value) &&
    'before' in value &&
    'after' in value
  );
}

/**
 * Flatten a payload into the rows the table renders. A before/after object becomes a
 * change row — that is the catalog criterion: "who raised this price?" has to read
 * as `5000 → 7500`, not as a nested JSON blob. Everything else is shown as it is.
 */
export function auditPayloadFields(payload: Record<string, unknown>): AuditPayloadField[] {
  return Object.entries(payload)
    .map(([key, value]) => {
      if (isChange(value)) {
        return {
          key,
          label: auditFieldLabel(key),
          before: value.before,
          after: value.after,
          change: true,
        };
      }
      return { key, label: auditFieldLabel(key), value, change: false };
    })
    .sort((left, right) => left.key.localeCompare(right.key));
}

/** Render one payload value as copy. An absent value is an em dash, not "undefined". */
export function formatAuditValue(value: unknown): string {
  if (value === null || value === undefined) {
    return '—';
  }
  if (typeof value === 'boolean') {
    return value ? 'ya' : 'tidak';
  }
  if (typeof value === 'string') {
    return value;
  }
  if (typeof value === 'number') {
    return String(value);
  }
  return JSON.stringify(value);
}
