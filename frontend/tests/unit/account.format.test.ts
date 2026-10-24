import { describe, expect, it } from 'vitest';

import { formatRemaining, formatSpeedLimit, historyStateLabel } from '$lib/domains/account';

describe('Account Status formatting', () => {
  const now = Date.parse('2026-03-01T08:00:00Z');

  it('renders the remaining Active Period in whole units', () => {
    expect(formatRemaining('2026-03-02T07:00:00Z', now)).toBe('23 jam');
    expect(formatRemaining('2026-03-02T09:00:00Z', now)).toBe('1 hari 1 jam');
    expect(formatRemaining('2026-03-01T08:30:00Z', now)).toBe('30 menit');
    expect(formatRemaining('2026-03-01T08:00:30Z', now)).toBe('kurang dari 1 menit');
  });

  it('reports a passed expiration instead of a negative duration', () => {
    expect(formatRemaining('2026-03-01T07:00:00Z', now)).toBe('sudah berakhir');
  });

  it('shows the download side of a rate limit', () => {
    expect(formatSpeedLimit('5M/5M')).toBe('5M');
    expect(formatSpeedLimit('10M/5M')).toBe('10M');
  });

  it('labels every Transaction state in Indonesian', () => {
    expect(historyStateLabel('pending')).toBe('Menunggu pembayaran');
    expect(historyStateLabel('paid')).toBe('Pembayaran diterima');
    expect(historyStateLabel('provisioned')).toBe('Selesai');
    expect(historyStateLabel('failed')).toBe('Gagal');
    expect(historyStateLabel('expired')).toBe('Kedaluwarsa');
  });
});
