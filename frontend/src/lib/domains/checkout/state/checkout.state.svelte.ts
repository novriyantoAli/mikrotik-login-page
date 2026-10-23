/**
 * UI-only checkout state: which channel the buyer picked and which Transaction
 * the screen is watching. Server data never lives here — TanStack Query owns
 * that (frontend skill §3, golden rule 6).
 */
export class CheckoutState {
  channel = $state<string | null>(null);
  transactionId = $state<number | null>(null);

  selectChannel(code: string) {
    this.channel = code;
  }

  watch(transactionId: number) {
    this.transactionId = transactionId;
  }

  /** Start a new Transaction after the watching one expired or failed. */
  clearTransaction() {
    this.transactionId = null;
  }
}

/** One instance per checkout screen, so two open tabs cannot share a selection. */
export function createCheckoutState(): CheckoutState {
  return new CheckoutState();
}
