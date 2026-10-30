# Checkout offers only the channels whose amount window fits the Package

The cheapest seeded Package is Rp 5.000 (`harian`). Every virtual account Tripay
offers starts at Rp 10.000, so checkout answered:

```
tripay: request rejected: /api-sandbox/transaction/create answered 400:
{"success":false,"message":"Minimum payment amount is Rp 10.000"}
```

…reported to the buyer as `502 dependency_failed` ("Pembayaran sedang tidak dapat
diproses"). Nothing was wrong with the gateway, the credentials or the signature:
the amount simply does not fit that channel. The adapter already dropped the
vendor's per-channel bounds (`minimum_amount`, `maximum_amount`) on the floor, and
`GET /transactions/channels` offered every closed channel regardless of the
Package.

## Decision

**1. The amount window is part of the channel.** `payment.Channel` gains
`MinAmountIDR`/`MaxAmountIDR`, parsed from the vendor's `minimum_amount`/
`maximum_amount`, and `Channel.Accepts(amount)` is the one rule both the listing
and the checkout use. Zero means "the vendor published no bound", not "zero
rupiah" — most QRIS channels start at Rp 1.000 and publish no maximum.

**2. The channel list is filtered by Package, server-side.** `GET
/transactions/channels?package_id=N` leaves out the channels whose window
excludes that Package's price; without the parameter it stays a plain list. The
filter lives here because the app has no numeric price to filter with: the public
catalog (ADR 0005/0012) is display copy (`"harga": "Rp 5.000"`), and asking the
app to parse rupiah would create a second price source of truth.

**3. Checkout refuses an unusable channel before the vendor call.** `resolve`
checks `Accepts(pkg.PriceIDR)` and returns `ErrChannelAmountUnsupported`, which
the handler maps to `400 invalid_request` with a message the buyer can act on
("Nominal paket di luar batas kanal pembayaran ini. Pilih kanal lain."). No
vendor bill is opened, no Transaction row is written, and the answer is no longer
a 502 that blames a dependency.

**4. The app asks for the filtered list and explains an empty one.** The checkout
passes the Package it was opened for, and renders "Tidak ada metode pembayaran"
with a way back instead of an empty picker with a pay button.

**5. "Closed payment" is the vendor's `type: direct`, not its group name.** The
old selector kept groups `Virtual Account` and `QRIS`, but Tripay files **QRIS
under `E-Wallet`** (`QRIS`, `QRISC`, `QRIS2`, `QRIS_SHOPEEPAY`), so the only channel
a Rp 5.000 Package can use was being dropped — fixing the amount filter alone
would have shown the buyer *nothing*. A channel is now closed-payment when it is
active, `type: direct` (not `redirect`), and either a virtual account or a
QRIS-prefixed code. Of the four QRIS aliases — same rail, same fee, acquirer-
specific names — only the canonical `QRIS` is offered (falling back to the first
variant if the merchant has no plain QRIS), because four indistinguishable
entries are not four choices.

## Alternatives rejected

- **Filter in the app.** It would need the numeric price, which the catalog
  deliberately does not expose (display strings only), so this means either a new
  price field or parsing rupiah — two ways to know one price.
- **Return every channel with a "usable" flag.** The app would still have to
  implement the same window comparison; the rule belongs with the money.
- **Let the vendor reject it.** That is the reported bug: a 502, an actionable
  message thrown away in `truncate`, and an unnecessary vendor round trip per
  attempt.
- **Reprice the seed so everything clears Rp 10.000.** Raising a Package price is
  a product decision, and the window belongs to the channel, not the catalog.

## Consequences

- `package_id` stays optional, so the endpoint remains a channel list for callers
  that are not checking out.
- A Package priced below every channel's window becomes a real, visible state:
  the buyer sees no methods and is told so. Today QRIS (from Rp 1.000) covers the
  Rp 5.000 Package.
- The live sandbox now yields six channels: five virtual accounts (min Rp 10.000)
  and one QRIS (min Rp 1.000). Rp 5.000 is offered QRIS only; Rp 25.000 gets all
  six.
- The channel group string is the vendor's (`E-Wallet` for QRIS), which the app
  displays as-is; it is a label, not a rule.
- The window is enforced twice on purpose: the listing hides what cannot work and
  the checkout refuses it anyway, because a cached or hand-made request can still
  ask for it.
- `make swagger-gen` was broken independently of this change when this decision
  was taken: three annotations named a `dto.*` type in files that never import
  their domain's `dto` package (`account/handler` and `health/handler`), so swag
  aborted and produced nothing. `docs/api/` is build output — it is gitignored, so
  nothing tracked was going stale — but the generated docs, and with them this
  route's new `package_id` parameter, only exist once the target runs. A follow-up
  made each of those annotations name a handler-local alias of the DTO, and CI now
  runs `make -C backend swagger-gen`, so the target cannot rot silently again.
