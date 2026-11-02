# A Transaction records the catalog terms it was sold at

`transactions` already snapshots what the buyer saw: `package_name` and
`amount_idr`, with the reason written on the field — "the amount the buyer was
shown must not change if the catalog is edited later". Two terms were left out,
and until now nothing could edit the catalog, so it did not matter.

`provisioning` reads the catalog live:

```
// backend/internal/application/transaction/service/provisioning.go:127
pkg, err := p.catalog.GetPackage(ctx, row.PackageID)
...
Expiration:  row.ProvisionTargetExpiration.UTC(),   // computed from pkg.DurationMinutes
RateLimit:   pkg.RateLimit,
```

So the granted **Active Period** and the `Mikrotik-Rate-Limit` a buyer receives
are the catalog's values *at provisioning time*, not the ones they paid for. The
sequence that breaks is ordinary once the **Admin Panel** exists:

1. Buyer pays Rp 25.000 for 7 days at 10M.
2. Owner changes the weekly Package to 5 days at 5M (a supplier cut, a promo
   ending, a typo fix).
3. The worker provisions the Transaction and writes 5 days at 5M to FreeRADIUS.

Nothing logs this as suspicious, because nothing knows the two numbers were ever
meant to be the same. `AmountIDR` stays correct, which makes it worse: the
receipt agrees and the account does not.

## Decision

**1. `transactions` gains `duration_minutes` and `rate_limit`, written at
checkout** from the Package the buyer is paying for — the same moment
`package_name` and `amount_idr` are written, for the same reason.

**2. Provisioning reads the Transaction, not the catalog.** `Apply` receives
`provisioning.Account{... Expiration, RateLimit}` built from the snapshot. The
`packages` service is no longer consulted on the provisioning path.

**3. `ProvisionTargetExpiration` keeps its role.** It is still computed once and
stored (ADR 0016), so a retry cannot recompute — now with an additional guard: it
cannot recompute from a different Package either, because the duration it is
computed from is frozen in the same row.

**4. Rows that predate the columns are backfilled by `migrate`**, the one-shot
command deployment already runs before the API (ADR 0012), filling from the
catalog for Transactions that are still `pending`. Rows that are already
`provisioned` need no backfill: their effect is in FreeRADIUS, and rewriting
their history would be inventing data.

**5. A missing snapshot is tolerated, not trusted.** If a checkout somehow stored
an empty duration, provisioning falls back to the catalog and logs it. The
fallback exists for one deploy boundary, not as a supported state.

## Rejected alternatives

- **Let the drift happen and document it.** A bug that only appears between
  payment and provisioning is the hardest kind to reproduce; the window is
  seconds in a quiet deployment and hours when the modem is unwell.
- **Forbid editing a Package while any Transaction is pending.** The operator
  loses control of the catalog because one buyer has not finished paying, and the
  refusal arrives at the worst moment — right after a supplier change.
- **Version the Package instead** (edit creates a new row, old row retired), and
  point the Transaction at the version. It is the most faithful model, and it
  duplicates the catalog on every price change; the snapshot gets the same
  guarantee for two columns.
- **Send duration and rate limit to FreeRADIUS from the checkout request.** The
  client would become the source of what it bought.

## Consequences

- The provisioning path no longer depends on the catalog's availability, which
  also removes a failure mode: a Package edited into an invalid state cannot
  break a buyer who already paid.
- `transactions` carries two more columns; the admin transaction list can show
  what was actually granted rather than what the catalog says now — the two
  agree, which is the point, and a disagreement becomes visible instead of
  invisible.
- The backfill runs inside the same `migrate` step that adds `packages.is_active`
  and `version` (ADR 0025), so one deploy carries every schema change this PRD
  needs.
