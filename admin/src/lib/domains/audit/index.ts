/**
 * Public surface of the audit domain. Other domains and routes import only from
 * here, never from schemas/, api/, queries/, state/ or components/ directly.
 *
 * There is no mutation to export: the trail is read-only by design (ADR 0027 §3).
 */
export { default as AuditFilters } from './components/AuditFilters.svelte';
export { default as AuditList } from './components/AuditList.svelte';
export { default as AuditLog } from './components/AuditLog.svelte';
export { auditKeys, createAuditListQuery } from './queries/audit.queries';
export { AuditFilterState, createAuditFilterState } from './state/audit.state.svelte';
export type { AuditSearch } from './state/audit.state.svelte';
export {
  AUDIT_ACTIONS,
  AdminAuditEntrySchema,
  AdminAuditFilterSchema,
  AdminAuditListResponseSchema,
  DEFAULT_AUDIT_PAGE_SIZE,
  MAX_AUDIT_PAGE_SIZE,
  auditActionLabel,
  auditActionLabels,
  auditFieldLabel,
  auditPayloadFields,
  formatAuditValue,
  targetTypeLabel,
  toAuditQueryParams,
} from './schemas/audit.schema';
export type {
  AdminAuditEntry,
  AdminAuditFilter,
  AdminAuditListResponse,
  AuditAction,
  AuditPayloadField,
} from './schemas/audit.schema';
