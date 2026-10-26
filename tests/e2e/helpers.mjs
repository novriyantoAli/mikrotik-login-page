import { execSync } from 'node:child_process';

/**
 * Shared plumbing for the two end-to-end specs.
 *
 * The journey needs three things no in-process test can provide: the real stack
 * (compose), the browser-facing Login Page, and the Magic Link — which only
 * exists in an undelivered SMS body, because `sessions.token_hash` is a one-way
 * hash and `magic_link_tokens` never stores the token itself (ADR 0011/0018).
 */

/** Origin of the RouterOS Login Page, served by the smoke static server. */
export const loginPageOrigin = () => process.env.E2E_LOGIN_PAGE_URL ?? 'http://localhost:4173';

/** Origin of the purchase app + API: the compose proxy, one origin (ADR 0009). */
export const appOrigin = () => process.env.E2E_APP_URL ?? 'http://localhost:8080';

/** The stub's /bayar page as the browser reaches it (host-published port). */
export const stubOrigin = () => process.env.E2E_STUB_URL ?? 'http://127.0.0.1:8082';

/**
 * The Phone Number the journey types and the canonical form the backend stores.
 *
 * A fresh number per run is not cosmetic: the Magic Link rate limit is one per
 * Phone Number per window, and it lives in a shared Redis with no test namespace
 * (`AUTH_RATE_LIMIT_PHONE_LIMIT`), so a fixed number would make the second run of
 * the same suite fail on a spent counter.
 */
export function uniquePhoneNumber() {
  const suffix = String(Date.now()).slice(-7);
  return { input: `0812${suffix}`, stored: `62812${suffix}` };
}

/**
 * The command that reads the outbox. It is overridable so the same spec can run
 * against a stack that is not the compose one (a LAN database, say):
 *
 *   E2E_PSQL_CMD='psql "postgres://user:pass@host:5432/db" -tAc "$E2E_SQL"'
 *
 * The SQL is passed through the environment rather than interpolated into the
 * shell, so no quoting accident can turn a Phone Number into shell syntax.
 */
const psqlCommand = () =>
  process.env.E2E_PSQL_CMD ??
  `docker compose -f docker-compose.yml -f docker-compose.e2e.yml exec -T postgres ` +
    `sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DATABASE" -tAc "$E2E_SQL"'`;

function queryMagicLinkBody(phoneNumber) {
  const sql =
    `select message from sms_outbox ` +
    `where phone_number = '${phoneNumber}' and state in ('pending', 'sending') ` +
    `order by id desc limit 1`;

  return execSync(psqlCommand(), {
    encoding: 'utf8',
    shell: '/bin/sh',
    stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, E2E_SQL: sql },
  }).trim();
}

/**
 * readMagicLink waits for the Magic Link SMS of one Phone Number and returns the
 * URL it carries.
 *
 * The row must still hold its body: only a `sent` row, or the final failed
 * attempt, clears `message` (notification/repository MarkSent/MarkFailed). The
 * E2E override keeps the modem absent and the retry budget long precisely so the
 * body survives until this read (ADR 0018 §5).
 */
export async function readMagicLink(phoneNumber, { timeoutMs = 45_000 } = {}) {
  const deadline = Date.now() + timeoutMs;
  let lastBody = '';

  while (Date.now() < deadline) {
    try {
      lastBody = queryMagicLinkBody(phoneNumber);
    } catch (error) {
      throw new Error(
        `Tidak bisa membaca sms_outbox lewat:\n  ${psqlCommand()}\n` +
          `Set E2E_PSQL_CMD kalau stack-nya bukan compose. Penyebab: ${error.message}`,
      );
    }

    const link = lastBody.match(/https?:\/\/\S+/);
    if (link) {
      return link[0];
    }
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }

  throw new Error(
    `Tidak ada Magic Link untuk ${phoneNumber} dalam ${timeoutMs} ms. ` +
      `Isi baris terakhir: ${JSON.stringify(lastBody)}`,
  );
}

/**
 * Every screen has to fit the narrowest width the PRD names (PRD point 29): the
 * document must never be wider than the viewport, so nothing hides behind a
 * horizontal scroll.
 */
export async function expectNoHorizontalScroll(page, label) {
  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));

  if (overflow.scrollWidth > overflow.clientWidth) {
    throw new Error(
      `${label} lebih lebar dari viewport (${overflow.scrollWidth} > ${overflow.clientWidth})`,
    );
  }
}
