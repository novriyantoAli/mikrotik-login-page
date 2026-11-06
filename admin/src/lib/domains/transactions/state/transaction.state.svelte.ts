import {
  DEFAULT_TRANSACTION_PAGE_SIZE,
  type AdminTransactionFilter,
  type TransactionState,
} from '../schemas/transaction.schema';

/** One submitted search: what the filter form reports and this state keeps. */
export interface TransactionSearch {
  state?: TransactionState;
  phoneNumber: string;
  from: string;
  to: string;
  packageId?: number;
}

/**
 * UI-only state: the search the operator has *submitted*, which is what the query
 * keys on. The form keeps its own draft inputs, so typing a Phone Number does not
 * fire a request per keystroke; only "Cari" changes this state.
 */
export class TransactionFilterState {
  state = $state<TransactionState | undefined>(undefined);
  phoneNumber = $state('');
  from = $state('');
  to = $state('');
  packageId = $state<number | undefined>(undefined);
  page = $state(1);
  pageSize = $state(DEFAULT_TRANSACTION_PAGE_SIZE);

  /** The submitted search, as the domain's API expects it. */
  get filter(): AdminTransactionFilter {
    return {
      state: this.state,
      phone_number: this.phoneNumber || undefined,
      package_id: this.packageId,
      from: this.from || undefined,
      to: this.to || undefined,
      page: this.page,
      page_size: this.pageSize,
    };
  }

  /** Whether this search is already the applied one (page aside). */
  matches(search: TransactionSearch): boolean {
    return (
      this.state === search.state &&
      this.phoneNumber === search.phoneNumber.trim() &&
      this.from === search.from &&
      this.to === search.to &&
      this.packageId === search.packageId
    );
  }

  /** Adopt a new search; a changed filter always starts at the first page. */
  apply(search: TransactionSearch): void {
    this.state = search.state;
    this.phoneNumber = search.phoneNumber.trim();
    this.from = search.from;
    this.to = search.to;
    this.packageId = search.packageId;
    this.page = 1;
  }

  reset(): void {
    this.state = undefined;
    this.phoneNumber = '';
    this.from = '';
    this.to = '';
    this.packageId = undefined;
    this.page = 1;
  }

  goToPage(page: number): void {
    this.page = Math.max(1, page);
  }
}

/** One instance per screen, so two mounted lists never share a search. */
export function createTransactionFilterState(): TransactionFilterState {
  return new TransactionFilterState();
}
