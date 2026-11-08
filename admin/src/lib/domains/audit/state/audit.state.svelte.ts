import {
  DEFAULT_AUDIT_PAGE_SIZE,
  type AdminAuditFilter,
  type AuditAction,
} from '../schemas/audit.schema';

/** One submitted search: what the filter form reports and this state keeps. */
export interface AuditSearch {
  adminEmail: string;
  action?: AuditAction;
  from: string;
  to: string;
}

/**
 * UI-only state: the search the auditor has *submitted*, which is what the query
 * keys on. The form keeps its own draft inputs, so typing an email does not fire a
 * request per keystroke; only "Cari" changes this state.
 */
export class AuditFilterState {
  adminEmail = $state('');
  action = $state<AuditAction | undefined>(undefined);
  from = $state('');
  to = $state('');
  page = $state(1);
  pageSize = $state(DEFAULT_AUDIT_PAGE_SIZE);

  /** The submitted search, as the domain's API expects it. */
  get filter(): AdminAuditFilter {
    return {
      admin_email: this.adminEmail || undefined,
      action: this.action,
      from: this.from || undefined,
      to: this.to || undefined,
      page: this.page,
      page_size: this.pageSize,
    };
  }

  /** Whether this search is already the applied one (page aside). */
  matches(search: AuditSearch): boolean {
    return (
      this.adminEmail === search.adminEmail.trim() &&
      this.action === search.action &&
      this.from === search.from &&
      this.to === search.to
    );
  }

  /** Adopt a new search; a changed filter always starts at the first page. */
  apply(search: AuditSearch): void {
    this.adminEmail = search.adminEmail.trim();
    this.action = search.action;
    this.from = search.from;
    this.to = search.to;
    this.page = 1;
  }

  reset(): void {
    this.adminEmail = '';
    this.action = undefined;
    this.from = '';
    this.to = '';
    this.page = 1;
  }

  goToPage(page: number): void {
    this.page = Math.max(1, page);
  }
}

/** One instance per screen, so two mounted readers never share a search. */
export function createAuditFilterState(): AuditFilterState {
  return new AuditFilterState();
}
