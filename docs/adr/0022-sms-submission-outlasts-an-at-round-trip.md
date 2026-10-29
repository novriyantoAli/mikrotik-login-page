# The message hand-off gets its own budget, longer than an AT round trip

Issue: after ADR 0019 the body was short enough to be accepted, and the dialogs
before it were healthy — `AT`, `AT+CMGF=1`, `AT+CSCS="GSM"`, even the
`AT+CMGS="628…"` prompt were answered with `OK`/`>`. Then `sms_outbox` row 15
recorded, at 01:19:17:

```
send sms: AT+CMGS body: no modem response within 10s (got "")
```

Nothing came back from the device for ten seconds, not even an `ERROR`. The row
was retried one minute later and was `sent` 13.5 seconds after the retry
started — so the modem was never broken: the same submission simply took longer
than `sms.command_timeout`. A body hand-off is not an AT round trip; the modem
only answers `+CMGS` once the SMSC has taken the message, and that wait is the
network's, not the modem's. Ten seconds is a command budget, and the hand-off on
that link sat close enough to it that the AT setup around it decided the
outcome.

The failure was a false negative, and false negatives are not free here: the
retry re-sent a Magic Link (or a PIN) that the first attempt may already have
delivered, so a slow network turned into a duplicate SMS. ADR 0010 accepts
at-least-once delivery, but a duplicate should be the rare crash case, not the
routine consequence of a too-tight deadline.

## Decision

**1. The message hand-off gets its own budget.** `sms.submit_timeout` (default
`60s`) bounds two things: writing the body with Ctrl-Z and waiting for
`+CMGS:`/`ERROR`/`+CMS ERROR`. `sms.command_timeout` (default `10s`, unchanged)
keeps bounding the AT setup: `AT`, `AT+CMGF`, `AT+CSCS`, the `AT+CMGS=` prompt,
and the `OK` after the reference number.

**2. The adapter takes the budget as a setting.** `modem.Settings.SubmitTimeout`
is carried by Fx from `sms.submit_timeout`; a zero value falls back to
`CommandTimeout`, so a test or an older wiring keeps its old behaviour instead of
timing out instantly.

**3. The deadline must stay inside the row's lease.** `Validate` rejects a
`sms.submit_timeout` that is not positive, or that is not shorter than
`sms.lease`. The lease is what says only one drainer owns the row (ADR 0010); a
send that outlives it would let a second claim start while the modem is still
working on the first.

**4. The timeout error names the budget that expired.** `no modem response within
1m0s` at `AT+CMGS body` reads differently from `within 10s` at `AT`, which is the
distinction an operator needs at 3am.

## Alternatives rejected

- **Raising `sms.command_timeout`.** Then every setup command waits a minute
  before reporting an unplugged or powered-off modem, and the worker sits on a
  dead device six times longer than it needs to.
- **No timeout on the body.** A wedged modem would hold the single serial session
  — and with it the drainer — forever.
- **Shorter retry backoff.** The retry is not the defect; the first attempt was
  fine and its deadline was wrong. A shorter backoff would only add duplicates
  faster.
- **PDU mode with concatenation (ADR 0019's rejected alternative).** Unrelated:
  this body fit one segment; the wait was the problem.

## Consequences

- A slow network no longer produces a routine duplicate SMS. A genuinely silent
  modem still fails, after up to `submit_timeout`, and the row backs off exactly
  as before.
- The drainer is single-threaded, so a stuck submission delays the rest of the
  batch by up to the new budget. That is the real cost, and it is bounded by the
  lease (2m default) rather than by the 20-message batch size.
- The worker's health beat (ADR 0018) lands after a pass, so a slow send pushes it
  out by up to `submit_timeout`: the gap between beats becomes sweep + submit
  (90s at the defaults) against a staleness window of 3 × `sweep_interval` (also
  90s). `/healthz` can therefore answer `stale sweeps` for one check during a slow
  submission; compose's ten-retry healthcheck absorbs that, and the row's lease
  (2m) still bounds how long the drainer can be busy. Lengthen `submit_timeout`
  much further and the sweep interval has to move with it.
- `SMS_SUBMIT_TIMEOUT` joins `.env.example`, `config.sample.yaml` and compose, and
  `defaults` carries `60s`, which `TestEveryConfigFieldHasADefault` enforces.
