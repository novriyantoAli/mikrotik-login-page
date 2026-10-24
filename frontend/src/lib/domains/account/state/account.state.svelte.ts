/**
 * UI-only Account Status state: whether the PIN is revealed and which history page
 * is open. Server data never lives here — TanStack Query owns that (golden rule 6).
 * The PIN is only a local reveal flag because the component renders what the
 * summary returned; it is never stored in a shared store.
 */
export class AccountState {
  pinVisible = $state(false);
  page = $state(1);
  pageSize = 5;

  togglePIN() {
    this.pinVisible = !this.pinVisible;
  }

  nextPage(totalCount: number) {
    if (this.page * this.pageSize < totalCount) {
      this.page += 1;
    }
  }

  previousPage() {
    if (this.page > 1) {
      this.page -= 1;
    }
  }
}

/** One instance per Account Status screen. */
export function createAccountState(): AccountState {
  return new AccountState();
}
