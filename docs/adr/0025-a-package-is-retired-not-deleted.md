# A Package is retired, never deleted

ADR 0012 made the catalog a deployment seed because nothing could write it. It
recorded the consequence in plain words: a **Package** deleted by hand is
re-created by the next deployment, "because the seed cannot tell a deletion from
never seeded". The **Admin Panel** is where catalog writes finally live, so that
consequence has to be settled rather than inherited.

Hard delete is the obvious feature and the wrong one:

- The seed keys on `packages.code` with `ON CONFLICT DO NOTHING`. A deleted row
  is a row the seed will happily insert again on the next deploy, so "delete"
  would mean "come back on Tuesday" — a behaviour nobody can explain to an
  operator who is sure they removed it.
- `transactions.package_id` points at the catalog row. A hard delete leaves
  history pointing at nothing, and the panel's own transaction list is one of the
  reasons the panel exists.
- A destructive button that is sometimes refused (because a Transaction exists)
  and sometimes accepted is a button with two meanings.

## Decision

**1. `packages` gains `is_active` (default true).** The panel deactivates and
reactivates; there is no delete endpoint at all. `GET /api/packages` filters
`is_active = true`, which is the only change to a contract the Login Page froze —
the payload shape is untouched, so the page needs no edit.

**2. A deactivated Package keeps its row, its `code`, and its history.** The
admin list shows it with a marker. The seed still finds the row and inserts
nothing, so the resurrection problem disappears by construction.

**3. Deactivating does not cancel a pending checkout.** Money that arrived is
still provisioned (ADR 0015/0016). The panel can stop selling; it cannot stop
honouring.

**4. The last active Package cannot be deactivated.** The Login Page would render
an empty list and "Beli" would have no target. The service refuses with `409` and
the panel says why.

**5. `code` is immutable after creation**, format `^[a-z0-9]+(-[a-z0-9]+)*$`,
unique. It is the seed's identity: editing it makes the seed treat the Package as
absent and insert a second row for the same product.

**6. Values are validated in the service, not only in the form**, because each of
them ends up in a bill or in FreeRADIUS verbatim:

| Field | Rule |
| --- | --- |
| `name` | 1–80 characters after trim, no control characters |
| `price_idr` | integer, `> 0`, `≤ 100_000_000` (a slipped zero is caught) |
| `duration_minutes` | `> 0`, `≤ 527_040` (366 days) |
| `rate_limit` | `^\d+(\.\d+)?[kKmMgG]?/\d+(\.\d+)?[kKmMgG]?$` |

`rate_limit` is written into `radreply` unchanged; a malformed value is an
account with the wrong limit, and nothing later in the pipeline would notice.

**7. Concurrent edits are optimistic.** `packages` gains `version` (integer,
incremented on every write). Reads return it, `PUT` requires it, a mismatch
answers `409` with the current row so the panel can show what changed. An
integer is preferred over `updated_at` because two writes can share a timestamp
at Postgres's resolution and a timestamp is a poor equality token.

## Rejected alternatives

- **Soft delete (`deleted_at`) like `transactions`.** It would work, but a
  retired product is not a deleted record — it is a product that stopped being
  sold and may be sold again. `is_active` says that, and it is reversible without
  touching the row's identity.
- **Hard delete gated on "no Transactions".** Two behaviours behind one button.
- **Hard delete plus a blacklist of deleted `code`s.** Invents a second source of
  truth for the catalog's identity to preserve a destructive action nobody needs.
- **`ON CONFLICT (code) DO UPDATE` in the seed** so the catalog always equals the
  code. ADR 0012 already rejected this: it silently reverts an operator's price
  on every deploy.
- **Last-write-wins on edits.** A price overwrite nobody saw is money lost with no
  witness.
- **Validation only in the panel's form.** The API is reachable directly, and the
  form is not the boundary.

## Consequences

- `is_active` is part of the admin contract but **not** the public one: the Login
  Page contract stays exactly as frozen, a filter is applied above it.
- The panel has no delete button, so the README's warning about hand-deleted
  Packages coming back can be retired; the seed's behaviour is now the correct
  behaviour for a retired Package.
- Retiring the last Package is refused, so an operator who really wants to stop
  selling must retire all but one — a deliberate speed bump, not an oversight.

## Amendment (2026-11-04, after the catalog write slice landed)

§4's refusal is evaluated inside the repository's transaction, not in the
service, because the last-active count and the row must be read under the same
lock: two admins retiring the last two Packages at once would otherwise both see
"one other is active" and commit. The service still owns the rule — it maps the
repository's `ErrLastActivePackage` to its own sentinel, and the transport owns
the `409` wording — so the repository decides only the fact, never the message.
The same holds for §7's version check: it is a conditional `UPDATE` under the row
lock, and a stale version returns the row as it is now so the panel can show what
changed instead of merely refusing.
