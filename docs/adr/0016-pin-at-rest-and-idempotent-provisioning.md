# PIN encrypted at rest; provisioning idempotent across Postgres and FreeRADIUS

A paid **Transaction** has to become a working hotspot account: a new 6-digit
**PIN** written to FreeRADIUS together with `Expiration` and
`Mikrotik-Rate-Limit` (ADR 0004/0001), the same **PIN** queued as an SMS
(ADR 0010), and the row moved `paid → provisioned` (PRD point 15). Three things
make that hard, and each one is decided here:

- the **PIN** must be shown again later (**Account Status**, and the completion
  screen of the purchase itself), so it cannot exist only in `radcheck`;
- the work touches two databases that share no transaction, and it is retried;
- the SMS row must commit together with the provisioning result (ADR 0010), while
  no domain service may hold a `*gorm.DB`.

## 1. The current PIN is encrypted ciphertext on `transactions`

`transactions` gains two columns: `pin_ciphertext BYTEA` and
`pin_key_version INTEGER`. No `accounts` table is introduced: PRD point 7 lists
five tables, and **Phone Number** is the only customer identity — a table keyed
by it would duplicate a truth FreeRADIUS already owns.

The current **PIN** for a **Phone Number** is the one on its newest
`provisioned` **Transaction**. That is the same row **Account Status** has to
show anyway, so "show me my PIN" is one indexed read, not a second store.

Encryption is AES-256-GCM with a random 96-bit nonce prepended to the
ciphertext. The key is deployment configuration and **versioned**:

```yaml
pin:
  # version=base64(32 bytes), comma separated, so a rotation keeps the old key
  # readable while new rows are written with the active one.
  encryption_keys: "1=<base64>,2=<base64>"
  active_key_version: 2
```

Every write stores the active version next to the ciphertext; every read selects
the key by the *stored* version and refuses an unknown one instead of silently
trying the active key (which would return a decryption error that looks like
corruption). A rotation is therefore: add the new key, bump
`active_key_version`, deploy, and keep the previous key configured until no row
carries its version anymore. **PIN** values never reach a log line — the masked
**Phone Number** and the **Transaction** reference are the only identifiers a
provisioning log entry carries.

One clear-text copy is deliberate and bounded: the outbox row. The modem needs
the digits, and delivery is at-least-once (ADR 0010), so `sms_outbox.message`
carries the **PIN** — and the **Magic Link** token — until the row is finished.
The body is blanked when the row reaches a terminal state (`sent`, or `failed`
after the last attempt), so after delivery only the ciphertext on
`transactions` holds the **PIN**. A pending row is the one window where a
database dump would expose an undelivered credential, and it closes by itself.

Rejected alternatives: storing the PIN in clear text on the Transaction (PRD 69
and one psql dump away from every account); storing only a hash (a hash cannot
be shown again, and CHAP needs a reversible value anyway — ADR 0004); a `pin`
table or `account` table (a second truth for a field that belongs to one
**Transaction**); encrypting with a single unversioned key (rotation becomes a
data migration instead of a config change); encrypting the outbox body too
(the modem is the only reader and the row is short-lived, so the extra key
handling would buy nothing over blanking it).

## 2. Provisioning inputs are persisted once, before anything is written to RADIUS

Postgres and FreeRADIUS MySQL share no transaction, so provisioning is made
idempotent by storing the two values a retry must not recompute — the target
`Expiration` and the **PIN** — in one guarded `UPDATE` before the first MySQL
write:

```sql
UPDATE transactions
SET provision_target_expiration = $2, pin_ciphertext = $3, pin_key_version = $4
WHERE id = $1 AND provision_target_expiration IS NULL AND pin_ciphertext IS NULL;
```

A retry (or a concurrent sweep) loses that guard, re-reads the row and uses the
stored values, so it applies the *same* expiration and the *same* **PIN**. The
order inside one attempt is:

1. compute the target `Expiration` **once**, from the **Package** duration and the
   `Expiration` FreeRADIUS currently holds (PRD point 21): an Active Period still
   in the future is extended, an expired or absent one starts at settlement time;
2. persist target and **PIN** with the guard above, then re-read;
3. upsert `radcheck`/`radreply` to the stored target (ADR 0001);
4. move `paid → provisioned` and insert the **PIN** SMS row in one Postgres
   transaction (decision 3).

A crash between 3 and 4 is harmless: the retry re-applies the same account. A
crash between 2 and 3 is harmless for the same reason. Nothing is "rolled back"
across the two databases — the stored target *is* the compensation, and that is
also why step 1 must never be recomputed after step 2: a second computation a
minute later would silently buy the buyer a minute of Active Period on every
retry.

## 3. `Outbox.EnqueueTx` takes an opaque database transaction handle

ADR 0010 requires the outbox row in the caller's transaction; ADR 0015 deferred
the helper to this slice. The shape chosen is (a) from the handoff: the handle is
type `database.Tx` in `internal/pkg/database`, an opaque wrapper only repositories
unwrap, plus `Repository.WithTx(ctx, fn)` on the Transaction repository and
`Outbox.EnqueueTx(ctx, tx, message)` on the notification service. The Transaction
service therefore composes "state + SMS" without importing `gorm` and without
reaching into the outbox table:

```go
return repo.WithTx(ctx, func(ctx context.Context, tx database.Tx) error {
    changed, err := repo.MarkProvisioned(ctx, tx, row.ID, at)
    if err != nil || !changed {
        return err // a concurrent attempt already notified the buyer
    }
    return outbox.EnqueueTx(ctx, tx, notification.Message{...})
})
```

The Asynq wake-up is enqueued while the transaction is still open, because the
service cannot know when the caller commits. That is safe in this system: a
wake-up that arrives before the commit (or after a rollback) makes the drainer
find nothing, and a wake-up that is lost only delays the SMS until the drainer's
next sweep. Redis is never the record — the row is.

Rejected alternative (b): letting the Transaction repository write `sms_outbox`
rows would put another domain's table inside this domain's repository, trade one
visible seam for a hidden one, and make the outbox's claim/lease rules reachable
from two write paths.

## 4. The PIN is returned by the completion read, not by a public endpoint

`GET /transactions/{id}` grows two fields: `phone_number` (always, it is the
hotspot username the completion screen has to show) and a nullable `pin`, filled
only when that **Transaction** is `provisioned`. The buyer's own session
authorizes the read (PRD 13/14), the same poll that already renders the
completion screen delivers the **PIN**, and no list endpoint ever carries one.

The same service method (`CurrentPIN`, newest `provisioned` **Transaction** of a
**Phone Number**) is what **Account Status** (#16) reuses for the show/hide
control, so the decryption path exists once. A separate `GET /account/pin`
endpoint was rejected: it would be a second read of the same column with a
second authorization surface to keep right.

## 5. The app keeps polling past `paid`

`paid` means "settlement received, provisioning not finished", so the checkout
now treats it as non-terminal and keeps watching until `provisioned` (or
`failed`/`expired`). The completion screen renders the **Phone Number**, the
**PIN**, the next step ("masuk ke hotspot dengan nomor ini dan PIN di atas"), and
a link back to the Login Page **only when** `return_url` is non-empty — the value
the backend already validated against the allowlist (ADR 0011), never a URL read
from the address bar.

`pin` is part of the shared contract fixtures; adding it means updating the Go
DTO, the zod schema and a `transaction.provisioned.response.json` fixture in one
change, or both contract suites fail (ADR 0015).

## 6. Provisioning runs in the worker, woken by a task and swept on a tick

The API keeps writing the durable record; the worker does the hardware-adjacent
work, exactly like the SMS drainer (ADR 0010):

- settlement (`pending → paid`) enqueues the payload-free task
  `transaction:provision`; it names no id, because the durable work item is "every
  `paid` **Transaction** that has not been provisioned yet";
- the worker also sweeps that same query on `pin.sweep_interval` (default 30s),
  so a lost Redis wake-up or an API crash right after settlement cannot strand a
  buyer;
- only the worker process holds the FreeRADIUS MySQL connection, the writer
  adapter and the modem. The API graph still *offers* the provisioning surface so
  integration tests can drive it through the same code, mirroring how the expiry
  surface is exposed for the frozen clock (ADR 0015).

Two concurrent attempts are safe by construction: the set-once guard (decision 2)
makes the second one reuse the first one's target and **PIN**, the guarded
`paid → provisioned` transition lets only one of them insert the SMS row, and the
FreeRADIUS upsert is by `(username, attribute)` rather than an append, so writing
the same account twice changes nothing.

FreeRADIUS writes stay the only vendor-shaped code: the upsert lives in
`internal/pkg/database` beside the connection, behind
`provisioning.Writer`, which the domain service already depends on. The adapter
updates first and inserts when no row matched, in one MySQL transaction, instead
of relying on a unique index the FreeRADIUS schema may not declare.
