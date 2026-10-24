/** Presentation helpers for Account Status. Values stay integers end to end. */

import type { HistoryState } from './schemas/account.schema';

const rupiah = new Intl.NumberFormat('id-ID', {
  style: 'currency',
  currency: 'IDR',
  maximumFractionDigits: 0,
});

/** Render an integer rupiah amount the Indonesian way ("Rp 25.000"). */
export function formatIDR(amount: number): string {
  return rupiah.format(amount).replace(/\u00a0/g, ' ');
}

/** Render an instant in the buyer's local time. */
export function formatDateTime(isoTimestamp: string): string {
  const moment = new Date(isoTimestamp);
  if (Number.isNaN(moment.getTime())) {
    return isoTimestamp;
  }
  return new Intl.DateTimeFormat('id-ID', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(moment);
}

/**
 * The remaining Active Period, in the largest whole units that still read well.
 * This is display only: whether it is "almost over" is the backend's
 * `active_period.state`, never this subtraction (ADR 0017).
 */
export function formatRemaining(isoTimestamp: string, now: number = Date.now()): string {
  const expiresAt = new Date(isoTimestamp).getTime();
  if (Number.isNaN(expiresAt)) {
    return isoTimestamp;
  }

  const remainingMs = expiresAt - now;
  if (remainingMs <= 0) {
    return 'sudah berakhir';
  }

  const totalMinutes = Math.floor(remainingMs / 60_000);
  if (totalMinutes < 1) {
    return 'kurang dari 1 menit';
  }

  const days = Math.floor(totalMinutes / (24 * 60));
  const hours = Math.floor((totalMinutes % (24 * 60)) / 60);
  const minutes = totalMinutes % 60;

  const parts: string[] = [];
  if (days > 0) {
    parts.push(`${days} hari`);
  }
  if (hours > 0) {
    parts.push(`${hours} jam`);
  }
  if (days === 0 && minutes > 0) {
    parts.push(`${minutes} menit`);
  }
  return parts.join(' ');
}

/** "5M/5M" is 5 Mbps down and up, so the page shows the download side. */
export function formatSpeedLimit(rateLimit: string): string {
  const [download] = rateLimit.split('/');
  return download.trim();
}

const historyStateLabels: Record<HistoryState, string> = {
  pending: 'Menunggu pembayaran',
  paid: 'Pembayaran diterima',
  provisioned: 'Selesai',
  failed: 'Gagal',
  expired: 'Kedaluwarsa',
};

/** Indonesian label for a Transaction state in the history. */
export function historyStateLabel(state: HistoryState): string {
  return historyStateLabels[state];
}
