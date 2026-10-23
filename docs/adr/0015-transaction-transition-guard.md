# Transaction transition guard and callback idempotency

A **Transaction** has exactly one path into `paid`: a gateway callback whose
signature over the raw payload verifies (ADR 0008). Two things then have to hold
under repetition and races: two "Bayar" taps must not open two bills, and two
deliveries of the same settlement notification must not move a **Transaction**
twice. Both are enforced by the database, not by a read-then-write in Go.

Decision: **guarded single-statement transitions.** The state machine lives in
one table (`entity.allowedTransitions`, PRD point 15) and every write names the
state it expects:

```sql
UPDATE transactions SET state = 'paid', paid_at = $2, updated_at = $2
WHERE id = $1 AND state = 'pending';
```

`RowsAffected` is the verdict. A repeated callback, a callback that lost a race
to another worker, and a callback arriving after the deadline all get
`RowsAffected = 0` and change nothing — no row locks, no `SELECT … FOR UPDATE`,
no in-process mutex that a second API replica would not share. This is the same
pattern as `used_at IS NULL` on Magic Link tokens (ADR 0011) and
`FOR UPDATE SKIP LOCKED` on the SMS outbox (ADR 0010).

Decision: **checkout idempotency is a partial unique index.** Postgres enforces
one live pending **Transaction** per **Phone Number** and **Package**:

```sql
CREATE UNIQUE INDEX idx_txn_pending ON transactions (phone_number, package_id)
WHERE state = 'pending';
```

Checkout first reads the live pending row and returns it with its instructions.
When two requests race past that read, both may open a vendor bill, but only one
insert wins; the loser re-reads the winner's pending row, returns it, and logs
the orphaned vendor reference so an operator can see the extra bill. The orphan
expires on its own at the gateway. A settled or expired **Transaction** leaves
the index, so the next purchase can start.

Decision: **the idempotency key is our `reference`, not the vendor's id.** For
example Tripay echoes `merchant_ref`, and that is the value the adapter returns
as `payment.Callback.Reference`; the vendor's own `reference` field is never
used to find a row. A callback naming a reference this system never issued is
acknowledged with `200` (so the gateway stops retrying) and logged; it never
creates anything. The signature is verified over the raw bytes before any field
is read, and the comparison is constant time.

Decision: **a mismatched amount does not move the state.** When a verified
callback says `paid` but its amount differs from the **Transaction**, the
callback is refused with `409` and logged with both amounts; the row stays
`pending` for an operator to reconcile. Marking such a payment `failed` would
hide money that was actually received, and marking it `paid` would provision an
account for the wrong amount.

Decision: **late settlement is acknowledged, not applied.** `paid → expired`,
`expired → paid` and `failed → paid` are not transitions, so a settlement that
arrives after the deadline or after a failure is acknowledged with `200`,
logged, and ignored. Recovering that money (refund or manual provisioning) is
outside this slice; the log line with the reference is the operator's hook.
Vendor `REFUND` maps to `failed` because this slice does not model refunds at
all.

Decision: **expiry is the backend's clock, and only its own.** `pending` rows
whose `expires_at` passed move to `expired` in one guarded `UPDATE` run by the
worker sweep (`transaction.expiry_sweep_interval`, default 1 minute) using the
injected `clock.Clock`. A page left open cannot expire its own **Transaction**,
and a client cannot keep one alive past its deadline. Checkout also closes the
overdue pending row of the *same* **Phone Number** and **Package** before opening
a new one, so the window between a deadline and the next sweep cannot turn a
legitimate second purchase into a unique-index failure; both paths are the same
guarded statement, scoped differently.

Decision: **reconciliation is deferred to #17.** `payment.Gateway.GetStatus`
exists on the port (ADR 0008) and is implemented by the adapter, but nothing
calls it yet: PRD point 16's periodic reconciliation for a lost callback is part
of the hardening slice, not of checkout. Until then a lost callback leaves the
**Transaction** `pending` until the deadline expires it; the buyer is told the
payment is being verified, and an operator has the reference in the log. The
reconciliation job must reuse `settle`/`close`, not add a second path into
`paid`.

Consequence for the app: polling stops at `paid` because this slice ends there.
After #15 adds `paid → provisioned`, the checkout may keep watching until
`provisioned`; that is a frontend change, not a state-machine change.

Decision: **the outbox transaction helper waits for #15.** ADR 0010 requires the
caller to write the outbox row in its own transaction when it has one. #14 never
writes an outbox row (provisioning and the PIN SMS belong to #15), so
`Outbox.EnqueueTx` is not introduced here; #15 adds it together with the
provisioning transaction that needs it.

Rejected alternatives: read-then-write with an in-memory check (two replicas,
two winners); `SELECT … FOR UPDATE` on the row before deciding (equivalent
outcome, more round trips); keying idempotency on the vendor's reference
(then a redelivery after a reference format change is a second purchase);
auto-failing on amount mismatch (hides received money); letting the client mark
`expired` (a closed tab would strand a paid **Transaction**).
