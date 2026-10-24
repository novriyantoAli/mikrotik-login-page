/**
 * UI-only Account Status state: whether the PIN is revealed. Server data never
 * lives here — TanStack Query owns that (golden rule 6). The PIN is only a local
 * reveal flag because the component renders what the summary returned; it is
 * never stored in a cross-domain store.
 */
export class AccountState {
  pinVisible = $state(false);

  togglePIN() {
    this.pinVisible = !this.pinVisible;
  }
}

/** One instance per Account Status screen. */
export function createAccountState(): AccountState {
  return new AccountState();
}
