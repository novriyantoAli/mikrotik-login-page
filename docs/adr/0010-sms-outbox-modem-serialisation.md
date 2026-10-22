# SMS leaves through a Postgres outbox drained by the one worker that owns the modem

Every SMS this system sends — a **Magic Link**, and later a **PIN** — is a row in
`sms_outbox`, written in the *same* Postgres transaction as the change that
produced it. Redis/Asynq carries only a wake-up task (`sms:deliver`); the row
carries the truth. A single drainer inside the worker process claims one row at a
time, hands it to the modem adapter (ADR 0003) behind
`notification.Sender` (`internal/application/notification/port.go`), and records
the outcome on the row.

Row states are `pending → sending → sent`, with `failed` plus an `attempts`
count and a `next_attempt_at` for the retry schedule. Delivery is at-least-once:
a crash between the modem's `OK` and the commit resends, so a duplicate SMS is
acceptable while a *missing* **PIN** is not. Nothing dedupes by message body —
only the row transition and the claim lock prevent a double send.

Chosen because the two hard requirements here are both transactional:

- **A paid buyer must not lose the PIN because the modem is off.** A direct AT
  call from a service would put seconds of blocking hardware I/O inside an HTTP
  request and would lose the message entirely on a crash; a row that stays
  `pending` is retried with backoff until the modem recovers, and the **PIN** is
  additionally visible in **Account Status** (PRD 14/24).
- **"Transaction provisioned but its SMS queued" must be one commit.** With the
  outbox in the same Postgres transaction as the account write, a provisioning
  job can never commit halfway into a state nobody will notify.

Serialisation has three layers, because each one alone is escapable:

1. **One writer process.** The API server never builds an Asynq server
   (`internal/server/worker/providers.go`), and the compose deployment runs a
   single worker replica — the modem physically exists on one host (PRD 59/60).
2. **One claim at a time.** The drainer selects the next due row with
   `FOR UPDATE SKIP LOCKED` and a lease column, so a second replica (or a stale
   task) cannot pick up the same row. The adapter additionally holds a
   process-wide mutex around the AT session, so no later caller — a test, a
   second drainer — can interleave `AT+CMGS` with another command.
3. **One connection.** The adapter keeps a single serial session (open, `ATE0`,
   `AT+CMGF=1`, `AT+CSCS="GSM"`), reopens it after a timeout, and fails the
   attempt rather than retrying blind inside the AT dialogue.

Asynq is deliberately *not* trusted for correctness: a task can be lost, so the
drainer also sweeps on a tick (rows older than the configured interval), and a
row stuck in `sending` past its lease returns to `pending`. That keeps the worker
correct with Redis empty of tasks, and keeps retry/backoff policy next to the
data instead of in queue options.

Consequences: `sms_outbox` is also the metric surface — pending depth, oldest
pending age, sent/failed counters per attempt — so "modem down" is visible
without reading logs; every attempt logs a structured line with the masked
**Phone Number**, the row id and the attempt number, never the **PIN** itself.
The worker gains config keys for the modem device, baud rate, AT timeouts, lease
and sweep interval, and max attempts. Test seam stays `testutil.FakeSMSSender`,
which records attempts instead of touching hardware, so the "modem unavailable →
row stays pending → delivered after recovery" behaviour is provable without a
modem.

Rejected alternatives: sending inline from the service (loses the message on
crash, blocks the request on hardware, and makes the caller's transaction span a
device); an in-memory queue or Redis list as the source of truth (an SMS for a
paid **Transaction** must survive a restart, and Redis is not the system of
record here); one Asynq task per SMS with Asynq's own retry as the only state
(nothing to reconcile after a lost task, nothing to observe, and queue
concurrency fights the single modem); a third-party SMS gateway (rejected
already in ADR 0003, and it would make this outbox unnecessary in the first
place).
