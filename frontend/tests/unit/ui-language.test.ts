import { readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

/**
 * PRD point 30 makes the domain glossary binding: the interface may not use the
 * words CONTEXT.md rejects. An acceptance criterion only a human can check rots
 * by the next commit, so the check runs here (ADR 0018).
 *
 * The scan is deliberately about *rendered* text: identifiers, Tailwind classes
 * and CSS (`order: 1`, `renewToken`) are not interface copy, and flagging them
 * would force an allowlist that grows until the test means nothing.
 */
const sourceDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../src');

/**
 * The words CONTEXT.md asks the interface to avoid, with the term it asks for
 * instead. `top-up` also matches "topup" and "top up", so no duplicate entries.
 */
const forbiddenTerms: readonly { term: string; use: string }[] = [
  { term: 'voucher', use: 'Package / Access Code' },
  { term: 'order', use: 'Transaction' },
  { term: 'renew', use: 'Extend' },
  { term: 'top-up', use: 'Extend' },
  { term: 'dashboard', use: 'Account Status' },
  { term: 'backoffice', use: 'Admin Panel' },
  { term: 'password', use: 'PIN' },
  { term: 'secret', use: 'PIN' },
  { term: 'purchase', use: 'Transaction' },
  { term: 'portal', use: 'Login Page' },
  { term: 'landing page', use: 'Login Page' },
  { term: 'member', use: 'User' },
  { term: 'username', use: 'Phone Number' },
  { term: 'otp', use: 'Magic Link' },
  { term: 'token', use: 'Magic Link' },
];

/** Attributes whose value a buyer reads on screen or through a screen reader. */
const userFacingAttributes = ['label', 'title', 'placeholder', 'aria-label', 'alt'];

/**
 * renderedText keeps only what a buyer can actually read in a `.svelte` file:
 * the text between tags plus the user-facing attribute values. Scripts, styles
 * and comments are dropped first, and so are Svelte blocks (`{#if token === ''}`,
 * `{:else}`, `{/if}`, `{@html …}`) and interpolations (`{pkg.nama}`) — those are
 * template logic, not copy, and reading them would flag ordinary identifiers.
 */
export function renderedText(source: string): string {
  const withoutCode = source
    .replace(/<script[\s\S]*?<\/script>/gi, ' ')
    .replace(/<style[\s\S]*?<\/style>/gi, ' ')
    .replace(/<!--[\s\S]*?-->/g, ' ')
    .replace(/\{[#:/@][^{}]*\}/g, ' ')
    .replace(/\{[^{}]*\}/g, ' ');

  const parts: string[] = [];

  for (const match of withoutCode.matchAll(/>([^<>]+)</g)) {
    parts.push(match[1]);
  }

  const attribute = new RegExp(`\\b(?:${userFacingAttributes.join('|')})="([^"]*)"`, 'gi');
  for (const match of withoutCode.matchAll(attribute)) {
    parts.push(match[1]);
  }

  return parts.join('\n');
}

function collectComponents(directory: string): string[] {
  const found: string[] = [];

  for (const entry of readdirSync(directory)) {
    const full = path.join(directory, entry);
    if (statSync(full).isDirectory()) {
      found.push(...collectComponents(full));
      continue;
    }
    if (entry.endsWith('.svelte')) {
      found.push(full);
    }
  }
  return found;
}

const components = collectComponents(sourceDir);

describe('interface language (PRD point 30)', () => {
  it('scans the whole app, not an empty list', () => {
    // A scanner that finds nothing passes for the wrong reason.
    expect(components.length).toBeGreaterThan(20);
    expect(components.some((file) => file.includes('domains/account'))).toBe(true);
  });

  it.each(forbiddenTerms)('never shows "$term" in rendered copy', ({ term, use }) => {
    const pattern = new RegExp(`\\b${term.replace('-', '[- ]?')}\\b`, 'i');

    const offenders = components
      .filter((file) => pattern.test(renderedText(readFileSync(file, 'utf8'))))
      .map((file) => path.relative(sourceDir, file));

    expect(offenders, `use "${use}" instead of "${term}"`).toEqual([]);
  });

  it('reads rendered copy only, not CSS, identifiers or template logic', () => {
    const source = `
      <script lang="ts">
        const order = { renew: 'topup' };
      </script>
      <style>
        .row { order: 1; }
      </style>
      <!-- dashboard is a word in a comment -->
      <div class="order-first" aria-label="Nomor Transaction">
        {#if voucher === null}
          <p>{member}</p>
        {:else}
          <h1>Voucher</h1>
        {/if}
      </div>
    `;

    const text = renderedText(source);

    expect(text).toContain('Voucher');
    expect(text).toContain('Nomor Transaction');
    expect(text).not.toContain('order');
    expect(text).not.toContain('renew');
    expect(text).not.toContain('topup');
    expect(text).not.toContain('dashboard');
    // Template logic is not copy: an identifier named after a banned word must not
    // fail the scan, or the list would need an allowlist to survive.
    expect(text).not.toContain('voucher');
    expect(text).not.toContain('member');
  });
});
