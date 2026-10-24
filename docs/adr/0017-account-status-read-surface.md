# Account Status reads three surfaces, computes "almost over" once, and shares one Extend rule

**Account Status** (CONTEXT.md) is the page that makes a lost SMS stop stranding
a buyer: it shows the active **Package**, the remaining **Active Period**, the
current **PIN**, and the **Transaction** history, and it is the door into
**Extend**. PRD point 6 is explicit that this is not a new domain with a table —
**Phone Number** is the only identity and FreeRADIUS already owns the credential
truth — so the page is a read composition of `transaction` (history, current
PIN), `provisioning` (Active Period end) and `packages` (the active Package).
Three decisions follow from that, and each one is made here.

## 1. Three read endpoints, and the PIN is only on the ones that need it

Decision: split the page into three reads instead of one fat payload.

- `GET /account` (session) →
  `{ phone_number, active_period: { expires_at, found, state }, package: { id, name, rate_limit } | null, pin }`
- `GET /transactions?page=&page_size=` (session) →
  `{ data: [...], total_count, page, page_size }`, **never** a `pin`
- `GET /account/extend-preview?package_id=` (session) →
  `{ package_id, current_expiration, estimated_expiration, extends }`

Only the summary carries the **PIN**, because only the summary is the "show me
my credential" surface. The paginated history is a list, and a list is what gets
cached, logged or copied around; a `pin` field there would multiply one six-digit
secret by every row of every buyer's history. `GET /transactions/{id}`
(ADR 0016) keeps its `pin` because it is the single-Transaction read the
completion screen already polls, and the app deliberately does not render it
there: the PIN is shown by **Account Status**, one place, with one show/hide
control.

`state` has four values, not three: `active`, `expiring`, `expired`, and `none`.
The handoff's three-value sketch cannot tell "never bought anything" (empty
state, catalog CTA) from "bought once, and it ran out" (expired state, buy CTA),
and the app needs both — so `found: false` pairs with `state: "none"` rather
than pretending an absent Active Period is an expired one.

Any **Phone Number** named in a request (query or body) is still only a
cross-check against the session: `/account?phone_number=…` and
`/transactions?phone_number=…` answer `404`, never `403`, so the endpoints cannot
be used to confirm that another buyer's data exists (PRD point 13). That is also
the lever the acceptance criterion asks for: "API menolak menampilkan PIN dan
riwayat milik Phone Number lain, dibuktikan lewat uji pada batas HTTP".

Rejected alternatives: one `GET /account` containing the history (the PIN would
ride along in a payload the app caches, and pagination would be impossible);
`GET /account/pin` (a second authorization surface for the same column);
returning the PIN in every Transaction row (multiplies the secret); a separate
`account` table (PRD point 7 forbids it, and it would duplicate FreeRADIUS).

## 2. The backend decides "almost over", with a threshold that must equal the Login Page's

Decision: deployment configuration `account.expiry_warning_hours` (default 24)
and the **backend** computes `active_period.state` from the injected
`clock.Clock`:

```
remaining = expires_at - now
remaining <= 0                      → "expired"
remaining <= expiry_warning_hours   → "expiring"
otherwise                           → "active"
```

The value **must equal** `expiryWarningHours: 24` in
`KOPINETGO-2024/config/data/voucher.js`, which `voucher-client.js` already uses
(`remainingMs <= warningMs → expiring`). The app renders `state` and never
computes it: no threshold crosses the wire and no clock comparison happens in
the browser, so the status page on the Login Page and the Account Status page in
the app cannot disagree about what "almost over" means. It also means a
deployment can move the boundary in one config key, and both surfaces move
together as long as the Login Page's data file is updated with it — the ADR is
the reminder that those two values are one value.

The comparison is `<=`, the same boundary the Login Page uses, so exactly 24
hours left counts as `expiring` on both sides.

Rejected alternatives: sending `expiry_warning_hours` to the app and computing
there (two clocks, two boundaries, drift); hardcoding 24 in Go and again in the
Login Page (two constants that silently diverge); using the Active Period
length from the last Package instead of a configurable warning window (a 30-day
Package would warn for a whole day, a 1-day Package never).

## 3. Extend arithmetic is one function used by provisioning and the preview

PRD point 21 says: an Active Period still running is extended
(`old expiration + Package duration`); an absent or finished one starts at the
settlement time (`settlement + duration`). `Provisioner.targetExpiration`
implemented that privately, and the Extend preview needs the exact same rule.
Copying the formula is how the two drift.

Decision: extract it into a pure function on the provisioning package —
`provisioning.Target(current Expiration, now, base time.Time, duration time.Duration) time.Time`
— and let both callers decide only their `base`:

- the provisioner passes `base = paid_at` (settlement, falling back to now),
  the value that must not be recomputed on a retry (ADR 0016);
- the preview passes `base = now`, because nothing is settled yet.

The preview is an estimate and says so: the final number is computed again at
provisioning time from the Active Period FreeRADIUS holds then, and it is the
stored one that is applied. But because both paths call the same function with
the same inputs in the normal case (still-running Active Period), the number the
buyer confirmed is the number that lands — a test provisions after a preview and
asserts equality rather than trusting the two call sites to stay in sync.

Rejected alternatives: calling `Provisioner` from the read path (a read would
need a writer, a PIN and a MySQL handle); a client-side formula in the app
(contradicts PRD point 21); modelling the preview as a dry-run `Transaction`
(writes a row for something nobody bought).

## 4. The PIN is hidden by default in the UI

The summary returns the current PIN because the whole point of **Account Status**
is that a lost SMS must not lock the buyer out. The app still renders it hidden:
a show/hide control, PIN never in a URL, never in a log line, never in a list
payload. Hiding it is a shoulder-surfing measure, not an authorization one —
authorization is the session, exactly as PRD point 14 says.

## Consequences

- The `account` package has no entity and no repository: it composes the
  `transaction` service, the `packages` service and the provisioning
  `ExpirationReader`. `README.md` and the module comment say so, so a later
  reader does not "fix" the missing table.
- `transactions` gains no column for this page. The repository gains one
  read — `ListByPhone(ctx, phoneNumber, ListParams)` — whose params type belongs
  to the repository, not to HTTP (no `dto.*` reaches persistence).
- A new configuration key must be added in four places, or
  `test/integration/deployment_test.go` fails: `config.go` defaults and
  validation, `config.sample.yaml`, `.env.example`, `docker-compose.yml`.
- The app's domain `account` (SvelteKit) owns its own schemas, api, queries and
  components; `/akun` is its route and the checkout links to it from the
  completion screen. No session or PIN state lives in a cross-domain store.
