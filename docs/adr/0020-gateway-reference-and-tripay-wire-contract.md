# The gateway's own reference is stored, because a status read is only accepted with it

Two defects in the Tripay adapter surfaced at once, both from the same blind spot:
the adapter was written against an assumed contract instead of the vendor's.

**1. Closed-payment creates were refused.** Tripay answered
`400 {"success":false,"message":"Undefined parameter signature"}` on every
checkout. The request carried no `signature` field at all, and `order_items` was
a JSON string where the vendor reads PHP-style nested form fields — with the
signature fixed, it answered `order items must be an array`.

**2. Reconciliation could never work.** `CreateTransactionResponse.Reference` was
mapped from `merchant_ref` (our reference), the vendor's own `reference` was
discarded, and `GetStatus` then asked `/transaction/detail?reference=<our ref>`.
Against the sandbox that answers `404 {"message":"Undefined parameter:
reference"}`; `?merchant_ref=` is answered the same way. So the sweep from
ADR 0018 would have logged a vendor rejection for every pending Transaction and
never settled a lost callback — the one failure mode it exists for.

## Decision

**The adapter follows the vendor's wire contract, and the domain keeps the
vendor's identity.** Concretely:

- `signature` is `hex(HMAC-SHA256(private_key, merchant_code + merchant_ref +
  amount))`, over the exact amount string that is sent. It is computed in the
  adapter (`createSignature`), because only the adapter knows the private key.
- `order_items` is sent as `order_items[0][sku|name|price|quantity]`, not as
  JSON: that is what the vendor parses from a form body.
- `payment.CreateTransactionResponse` gains `GatewayReference` (the vendor's
  `reference`); `payment.Gateway.GetStatus` takes *that* id, and its doc says so.
  `Transaction` gains `gateway_reference` (AutoMigrate adds the column), written
  at checkout and used by the reconciliation sweep. Our `reference` stays the
  buyer-facing identity the callback echoes as `merchant_ref`.
- A row without a gateway reference (written before the column existed) is
  reported and skipped, not queried with a reference the vendor will reject.

Both fakes (`testutil.FakeGateway` and the transaction service's) now hand back a
distinct vendor id and translate it on lookup, exactly as Tripay does, so a sweep
that passes the wrong field fails in tests instead of only in production. The
Tripay tests pin the create signature digest and the status query parameter
against the documented contract.

## Alternatives rejected

- **Look the payment up by `merchant_ref`.** Not offered by the vendor for a
  detail read, verified against the sandbox: both parameter names answer
  `Undefined parameter: reference`.
- **Derive the vendor id again later.** It only exists in the create response;
  a list-and-match call would add a second vendor round trip per polled row and
  pagination for information already in hand.
- **Return the vendor id to the app.** It is not the buyer's identity and the
  callback carries `merchant_ref`; the browser has no use for it.

## Consequences

- Deployment must run the migration (`make run-migrate`, or the one-shot migrate
  container) before the new API serves traffic, or the column is missing.
- Two names now exist per payment on purpose: `reference` (ours, in the API, the
  callback and the logs) and `gateway_reference` (the vendor's, internal only).
  Confusing them is the bug this ADR records, so the port's doc names the
  distinction explicitly.
- The adapter tests are the contract tests: a signature or parameter change that
  the vendor would reject now fails in `make test`, not in a buyer's checkout.
