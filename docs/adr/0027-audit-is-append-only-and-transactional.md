# Every panel write and PIN reveal leaves one audit row, written in the same transaction

The panel introduces two kinds of action nobody can take back: changing what a
**Package** costs, and revealing a buyer's **PIN**. Both are exactly the actions
an operator will one day be asked about — "who raised this price?", "who looked
at this customer's PIN?" — and today there is nowhere to ask.

The obstacle is not the table, it is the boundary. The domain that writes the
catalog is `packages`; the domain that owns admin identity is `admin`. If the
handler writes the change and *then* records the audit, a crash, a cancelled
request or a database blip between the two leaves a change with no witness — the
one failure mode an audit trail must not have. Making `packages` depend on the
`admin` domain is the other easy mistake: the catalog would then know about
admins, which is backwards.

## Decision

**1. Audit is a port, not a dependency on `admin`.** The interface lives in
`internal/pkg/audit`, beside the vocabulary, and every domain depends on that one
declaration:

```go
// Recorder is what a domain depends on to leave a trail for a privileged action.
// The admin domain implements it; nothing here imports admin.
type Recorder interface {
    Record(ctx context.Context, tx database.Tx, entry Entry) error
}
```

The adapter lives in `admin` and is injected by Fx — the same inversion the
repo already uses for `eventbus.Publisher` (backend skill §8.3).

**Amendment (2026-11-04, after the Admin Panel's identity slice landed):** this
section originally had each consumer declare its own copy of the interface
(`packages/repository/audit.go`, `transaction/repository/audit.go`). Two identical
declarations and a composition root returning a tuple of them meant a third
consumer (#32) would add a third copy. The interface now lives in
`internal/pkg/audit` and is bound once; the reason this section exists is
unchanged — no domain imports `admin`, and `admin` imports no domain. It also
gains the caller's transaction in its signature, and `database.WithinTx` is now
the only way to build a `database.Tx`, so §2's "inside the caller's transaction"
is a property of the type rather than a runtime check.

**2. The insert happens inside the caller's database transaction.** The
repository that mutates the row takes the `Recorder` (or is handed a
transactional handle) so the catalog UPDATE and the audit INSERT commit or roll
back together. A write that succeeds without its row is not representable, and
since `WithinTx` is the only constructor of a non-zero `Tx`, a pool handle cannot
be mistaken for one.

**3. The table is append-only.** `admin_audit_log`:

| Column | Purpose |
| --- | --- |
| `id`, `created_at` | when |
| `admin_id`, `admin_email` | who — the email is copied so the row still reads after the admin is gone |
| `action` | `package.create`, `package.update`, `package.activate`, `package.deactivate`, `pin.reveal`, `admin.create`, `admin.update`, `admin.password_reset`, `admin.deactivate` |
| `target_type`, `target_id` | what |
| `payload` (JSONB, small) | before/after for catalog fields; the Transaction reference for a PIN reveal |
| `ip` | where from |

There is no update endpoint, no delete endpoint, and no "purge" action in the
panel.

**4. A PIN reveal is audited whether or not the reveal succeeds.** The attempt is
the interesting fact; a failed decryption is worth seeing too, and it is recorded
with the failure, never with a plaintext PIN.

**5. Reading the trail is itself role-gated.** `owner` and `auditor` read it;
`support` does not, so the person whose actions are most likely to be reviewed
cannot study the review.

## Rejected alternatives

- **Application logs (`zap`) as the audit trail.** They rotate, they do not
  survive a restart in a queryable form, and an operator cannot answer "what did
  this admin do last week" without shell access to the container.
- **Audit written after the fact, best-effort.** Cheaper, and it is precisely the
  design that loses the row when it matters.
- **Audit only for PIN reveals** because prices "are visible in the catalog". A
  current price is not a history, and the money question is about the change.
- **A required free-text reason for each PIN reveal.** Looks like rigour; in
  practice it produces "cek" typed fifty times. The actor, the target and the
  time are the facts that survive scrutiny; a reason field would only add noise
  to them.
- **A generic event stream reused for everything.** The panel's trail has a
  different reader, a different retention question, and a different shape from
  domain events; one table that answers the operator's question beats a bus that
  nobody queries.

## Consequences

- `packages` and `transaction` gain a constructor parameter; their unit tests
  pass a recording fake and assert one entry per write.
- A failed write must produce **no** audit row — the same transaction covers both
  directions, and there is a test that a validation failure leaves the table
  empty.
- Retention and rotation are out of scope for v1. The table grows with panel use,
  which for a hotspot deployment is small; the PRD records the omission rather
  than pretending a policy exists.
