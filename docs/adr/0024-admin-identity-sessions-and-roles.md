# Admin identity is separate from the buyer's Phone Number, with three roles

The buyer's only identity is a **Phone Number** and the only way in is a
**Magic Link** over SMS (ADR 0004, ADR 0011). That is the right shape for a
hotspot customer and the wrong shape for the person who edits prices.

Three facts force a second identity:

- The operator must be able to open the panel when the modem is down. ADR 0022
  and ADR 0023 exist because the SMS path hurts: a hand-off that outlasts an AT
  round trip, a modem refusal that carries its own cause. A panel that signs in
  through that path is unavailable exactly during the incident it is needed for.
- Two panel actions are money and credentials: changing a **Package** price and
  revealing a **PIN**. Both need an actor that is stable and revocable — a phone
  number can be borrowed, handed over, or SIM-swapped.
- A buyer session must never become an admin session. The purchase app and the
  panel share one origin (ADR 0009, ADR 0013), so the separation has to be
  structural, not a `role` column a lookup might forget to filter on.

## Decision

**1. Admins are their own entity.** `admins` holds `email` (unique, stored
lowercase), `password_hash` (bcrypt, cost 12), `role`, `is_active`. Passwords
are minimum 12 characters with no composition rules and are rejected when equal
to the email; length beats punctuation, and forced rotation is not asked for.

**2. Sessions are their own table and their own cookie.** `admin_sessions`
mirrors the buyer's hashed-token shape (ADR 0011) but is a separate table, and
the cookie is separate too: `HttpOnly; Secure; SameSite=Strict;
Path=/api/admin`, absolute TTL `ADMIN_SESSION_TTL` (default 12 hours), no sliding
renewal. The admin middleware reads only `admin_sessions`; the buyer middleware
reads only `sessions`. A bug in one lookup cannot escalate into the other.

`SameSite=Strict` is affordable here because the panel has no cross-site entry
point: the login form is served by the panel itself. It closes CSRF without a
second token mechanism.

**3. Three roles, enforced per route on the server.**

| Action | owner | support | auditor |
| --- | --- | --- | --- |
| Read the catalog (including retired Packages) | ✓ | ✓ | ✓ |
| Write the catalog | ✓ | ✗ | ✗ |
| Read Transactions | ✓ | ✓ | ✓ |
| Reveal a PIN (audited) | ✓ | ✓ | ✗ |
| Read active subscribers + verify against FreeRADIUS | ✓ | ✓ | ✓ |
| Manage admins | ✓ | ✗ | ✗ |
| Read the audit log | ✓ | ✗ | ✓ |
| Change own password | ✓ | ✓ | ✓ |

`support` reveals PINs because that is the job it exists for (a buyer calls
because the SMS is lost) and does not touch prices. `auditor` reads and never
reveals, because the PIN is access to somebody else's account. The audit log is
owner+auditor only, so support cannot study its colleagues' trail.

The UI hides what a role cannot do; the server is what decides. Every cell is
covered by an HTTP-level test, so a `403` is proven to come from the backend.

**4. Login is rate-limited per IP and per email, with uniform errors.** Reuses
the existing limiter shape (`ADMIN_RATE_LIMIT_IP_*`,
`ADMIN_RATE_LIMIT_EMAIL_*`). Unknown email and wrong password answer the same
status and body, so the endpoint cannot enumerate admins. There is no lockout:
locking the operator out of the panel during an incident is a worse failure than
the guessing the rate limit already bounds.

**5. Password reset and deactivation revoke every session of that admin at
once.** Revocation is a server-side fact, not a cookie the browser is trusted to
drop.

**6. The first owner is seeded by `migrate`.** Same contract as the catalog seed
(ADR 0012): one command, run before the API starts, idempotent, insert-if-missing
keyed on the unique email. `ADMIN_EMAIL` + `ADMIN_PASSWORD`; when the table is
empty and either is missing the command fails loudly, and when any admin exists
the variables are ignored entirely — a deploy must never reset a password.

## Rejected alternatives

- **Admins as Phone Numbers on an allowlist**, signing in with the existing
  Magic Link. No new credential to manage, but the panel becomes unreachable
  whenever the modem or SMSC is the thing that broke — the one moment it matters.
- **One `sessions` table with a `kind` column.** Less code, and every read path
  must remember the filter; the failure mode of forgetting is silent escalation.
- **Creating admins by hand in SQL before first use.** Leaves the bootstrap
  undocumented and gives a deploy with an empty `admins` table no diagnostic.
- **A "create the first admin" form in the panel.** Whoever opens the panel first
  owns the system.
- **Tokens in `localStorage`.** The cookie is already opaque, hashed at rest, and
  invisible to JavaScript (ADR 0009); a stored token would be a regression.
- **MFA/TOTP in v1.** Worth doing, but it is a second factor on a panel with one
  operator and one audit trail; recorded as a candidate, not smuggled in.

## Consequences

- `AUTH_*` and `ADMIN_*` configuration are distinct; a deployment that forgets
  the admin keys fails at `migrate`, not at first login.
- Every new admin endpoint declares a required role next to its route, so the
  matrix in this ADR and the middleware table are the same list in two places —
  the HTTP tests are what keep them honest.
- The buyer's `sessions` table and the panel's `admin_sessions` table share a
  shape but not a lifetime: losing a buyer session is a support ticket, losing an
  admin session is a re-login.
