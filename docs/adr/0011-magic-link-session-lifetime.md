# A Magic Link is a hashed single-use token; the session it opens is a hashed opaque cookie

A **Magic Link** is 32 random bytes from `crypto/rand`, base64url-encoded, sent to
the **Phone Number** by SMS (ADR 0010). Postgres stores only its SHA-256 hash
plus the purpose it was issued for (`checkout` with a chosen **Package**, or
`account`), the optional allowed `return` target, `expires_at` (default 15
minutes, configurable) and `used_at`. Redemption is a single atomic statement —
`UPDATE magic_link_tokens SET used_at = now() WHERE token_hash = $1 AND used_at IS
NULL AND expires_at > now() RETURNING ...` — so two taps on the same link, an
expired link and an unknown link all take the same losing branch and answer the
same "tautan tidak berlaku" message. The token in the row is never the token in
the SMS.

Redemption then mints a session: 32 random bytes, base64url, set as an
`HttpOnly`, `SameSite=Lax`, `Secure` (configurable off for local) cookie with
`Path=/`, no `Domain`, and stored in Postgres as SHA-256 hash with the
**Phone Number**, `created_at`, `expires_at` (default 30 days, configurable) and
`revoked_at`. Sign-out deletes the cookie *and* sets `revoked_at`, so a copied
cookie stops working immediately — the property ADR 0009 was chosen for. The
session cookie is the only authorization for user endpoints; a **Phone Number**
in a URL or body must equal the session's, and a mismatch is answered `404`
rather than `403` so the response never confirms that another number exists
(PRD 13), while the current **PIN** is readable through the session precisely so
a lost SMS never locks out a paying buyer (PRD 14).

Both token kinds are hashed with plain SHA-256, not bcrypt/argon2: they are
256-bit uniformly random values carried in a URL or a cookie, so there is no
dictionary to attack and a dump of these tables yields nothing usable, while a
per-request slow hash would tax every authenticated call for no gain. This is
deliberately the opposite of the **PIN**, which is 6 digits and must be shown
again in **Account Status**, so it is stored encrypted with a versioned
deployment key (PRD 7) rather than hashed.

The request endpoint is `POST /api/auth/magic-link` with the **Phone Number** and
optional `package`/`return`. It always answers with the same success body and
status whether or not that number has ever transacted, so it cannot be used to
enumerate customers. Rate limits are Redis counters — one window per **Phone
Number** (hashed as the key) and one per client IP, taking the IP from
`ClientIP()` under the configured proxy CIDRs (`server.trusted_proxies`) — and a
repeat inside the window sends at most one SMS, answering success like the
first. Only a *refused* request answers with the stable `rate_limited` code from
`internal/pkg/httpx`.

`return` is validated when the link is *requested*, not when it is followed: an
absolute URL is kept only if it matches the configured Login Page allowlist,
otherwise it is dropped and the token simply carries no return target. The
exchange handler therefore never sees an attacker-supplied redirect, and a
compromised token cannot be turned into an open redirect.

Consequences: the auth domain owns `sessions` and `magic_link_tokens` and never
hands a raw token to a log line; expiry decisions come from the injected
`clock.Clock`, so "expired link" and "expired session" are testable without
waiting. Config keys added for the slice: magic-link TTL, session TTL, cookie
`secure`, per-phone and per-IP window and limits, and the Login Page allowlist.
The frontend stays as ADR 0013 fixed it — a static SPA on the same origin that
lets the browser carry the cookie and never reads it, so there is no BFF
`+server.ts` holding a Bearer token and no token in JavaScript at all.

Rejected alternatives: a signed JWT (no revocation, and ADR 0009 already argued
against a JS-readable bearer token); storing the token itself in Postgres
(a database leak would hand out live sessions and links); letting the SMS
**PIN** authorize **Account Status** (breaks the "lost SMS never strands a
buyer" requirement in CONTEXT.md); `SameSite=Strict` on the session cookie (the
Magic Link arrives as a top-level navigation from the SMS app, and Strict would
make that navigation look signed-out); bcrypt for high-entropy tokens (cost
without a threat model to justify it); and validating `return` only at redirect
time (leaves an open-redirect path one refactor away).
