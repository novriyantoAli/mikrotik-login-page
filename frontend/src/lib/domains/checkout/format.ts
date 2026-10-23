/** Presentation helpers for money and deadlines. Values stay integers end to end. */

const rupiah = new Intl.NumberFormat('id-ID', {
  style: 'currency',
  currency: 'IDR',
  maximumFractionDigits: 0,
});

/** Render an integer rupiah amount the Indonesian way ("Rp 25.000"). */
export function formatIDR(amount: number): string {
  return rupiah.format(amount).replace(/\u00a0/g, ' ');
}

/** Render a channel fee the way the picker shows it. */
export function formatChannelFee(feeFlatIdr: number, feePercent: number): string {
  if (feePercent > 0) {
    return `${feePercent.toLocaleString('id-ID')}%`;
  }
  if (feeFlatIdr > 0) {
    return formatIDR(feeFlatIdr);
  }
  return 'Gratis';
}

/** Render the payment deadline in the buyer's local time. */
export function formatDeadline(isoTimestamp: string): string {
  const moment = new Date(isoTimestamp);
  if (Number.isNaN(moment.getTime())) {
    return isoTimestamp;
  }
  return new Intl.DateTimeFormat('id-ID', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(moment);
}
