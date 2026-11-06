import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

/** Merge Tailwind classes without duplicate-conflict surprises. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

/**
 * Turn zod issues into one message per form field. Both the request schema and
 * the form use the same schema, so the message a field shows is the message that
 * would have refused the request (skill §11).
 */
export function fieldErrorsFrom<T extends string>(
  issues: ReadonlyArray<{ path: PropertyKey[]; message: string }>,
): Partial<Record<T, string>> {
  const errors: Partial<Record<T, string>> = {};
  for (const issue of issues) {
    const field = issue.path[0] as T | undefined;
    if (field && !errors[field]) {
      errors[field] = issue.message;
    }
  }
  return errors;
}

/**
 * Rupiah for a panel screen. Money is always integer rupiah on the wire; this is
 * the one place it becomes display copy, so a formatting change is one edit
 * instead of one per domain.
 */
export function formatPriceIDR(amountIDR: number): string {
  return new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR' }).format(amountIDR);
}
