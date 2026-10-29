# A refusal from the modem carries the modem's own cause

Issue: on 27 Okt 11:39 `sms_outbox` row 16 recorded

```
send sms: AT+CMGS body: modem answered ERROR
```

twenty-six seconds after the drainer claimed it. The next attempt, one minute
later, delivered the same body. So the body was fine and the delay was the
network's. But the log said only `ERROR` — the modem's catch-all, sent with
extended error reporting off, which is the factory default. An operator reading
that line cannot tell

- the SMSC or the network refusing for now (`+CMS ERROR: 500`, `331`, `38`),
- the SIM refusing (`+CMS ERROR: 10` not inserted, `13` failure, `14` busy),
- the destination being unacceptable (`21`, `28`),
- or this dialogue being broken.

Night one (ADR 0022) was the same defect in a different costume: the line named
the step but not the cause, and the answer had to be reverse-engineered from the
timeline. ADR 0019 exists for the same reason.

## Decision

**1. The session turns numeric error reports on.** `ensureTransport` sends
`AT+CMEE=1` after `ATE0`, best effort: a firmware that rejects it is
debug-logged and the dialogue continues. It is re-sent on every device open
because it is modem state, not port state, and a power-cycled modem resets it.

**2. `+CME ERROR` is part of the AT failure vocabulary.** It joins `ERROR` and
`+CMS ERROR` in `failureAnswers` and in every `expect`, so an enabled modem's
refusals are recognised instead of being read as "no response". `matchToken`
already prefers the earliest match, which is why `+CME ERROR: 3` reports
`+CME ERROR` and not the `ERROR` inside it.

**3. A refusal keeps the modem's own code.** The rest of the refusal line is
consumed into the error: `AT+CMGS body: modem answered +CMS ERROR: 500`. The
number is left untranslated on purpose — 3GPP TS 27.007 §9.2.3.22 codes are
stable, a table of prose per firmware would not be. Consuming the line also stops
it from answering the next command in a best-effort step.

**4. The failure line carries `elapsed`.** A refusal after twenty seconds of
network wait is a different fault from one that comes back at once, and nobody
should have to reconstruct that from two log timestamps again.

**5. The retry policy is untouched.** A refusal stays retryable. Row 16 was
refused and the one-minute retry delivered it, which is exactly what the outbox of
ADR 0010 is for. Only a body that cannot be encoded or fitted in one segment is
final (ADR 0019), because that one is deterministic.

## Alternatives rejected

- **Treating a refusal as unsendable.** It would have dropped row 16 — a
  deliverable Magic Link — on the first transient `500`.
- **Mapping codes to prose in code.** A table that goes stale per firmware
  revision, next to a standards document the operator can read.
- **Probing `AT+CREG?`/`AT+CSQ` before every message.** An extra round trip on
  every send to answer a question only a failure asks; the code in the error
  already separates network from SIM from destination.

## Consequences

- The operator line is now self-contained: `modem dialog failed {"elapsed":
  "26.1s", "error": "AT+CMGS body: modem answered +CMS ERROR: 500"}` says the
  network refused after a long wait, without a second data source.
- Modems that ignore `AT+CMEE` keep working unchanged; the vocabulary still
  reads `ERROR`, so old log lines stay comparable.
- Cost is one command per device open, not per message, and one `DEBUG` line on
  firmware that refuses it.
- `ERROR` is still matched, so a session that lost CMEE (power cycle mid-run) is
  diagnosed no worse than before.
