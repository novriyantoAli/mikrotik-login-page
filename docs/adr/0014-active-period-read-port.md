# Read the Active Period through the provisioning port; unknown means empty

`GET /active-period?phone=<Phone Number>` (ADR 0005) answers with the account's
`Expiration`, which lives in the FreeRADIUS MySQL schema (ADR 0001/0002), not
in Postgres. The backend is a writer of that schema, never its owner.

Decision: the read is a port next to the existing FreeRADIUS writer —
`provisioning.ExpirationReader` in `internal/application/provisioning/port.go`
— and its MySQL adapter lives beside the FreeRADIUS connection in
`internal/pkg/database`. The `activeperiod` domain's service depends on the port,
never on `database/sql` or a table name, so the Login Page contract can be tested
with a fake reader exactly like the payment gateway and the SMS sender, and the
adapter is the only code that knows which FreeRADIUS table and attribute hold
the expiry.

Behaviour, chosen so the status page degrades instead of failing:

- A **Phone Number** with no `Expiration` row answers `200 {"data":{"expiry":null}}`.
  "This account has no Active Period" is an answer, not an error, and the Login
  Page already renders itself as unavailable for a null expiry.
- FreeRADIUS MySQL being unreachable answers `503` with the standard error
  envelope (`dependency_unavailable`). The status page then shows "tidak dapat
  dimuat" rather than inventing an expiry or reporting an unknown account.
- The endpoint stays public and read-only: no session, no writes, and no
  distinction between "never bought" and "number is not ours" in the response.

The **Phone Number** is normalised to the canonical `628…` form with the one
shared domain function before the lookup, because FreeRADIUS stores the username
in that form (PRD point 9). Accepting `08…`, `+62…` and spaced variants is the
issue's acceptance criterion; any other input is a `400` with
`invalid_phone_number`, so a typo never looks like "no Active Period".

Both reads also answer `Access-Control-Allow-Origin: *` for safe methods, with
credentials disallowed. RouterOS serves the Login Page from each hotspot's own
address, so an origin allowlist cannot be written in advance; the payloads are
public and session-free, which makes the wildcard equivalent to what `curl`
already does. This does not weaken ADR 0009: the purchase app is same-origin,
and a cross-origin page still cannot read a user-specific response or preflight
a state-changing one.

Rejected alternatives: querying `radcheck` from the handler (leaks the vendor
schema into the transport and makes the contract untestable without MySQL);
answering `404`/`500` for an unknown number (the Login Page would show a load
failure instead of an empty state); reading Postgres (it does not hold the
**Active Period**; duplicating it would create a second truth).
