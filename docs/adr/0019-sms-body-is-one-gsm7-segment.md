# An SMS body is one GSM-7 segment, and a body that is not is refused before the modem

Issue: `sms_outbox` rows 12 and 13 sat `pending` with `attempts` climbing and
`last_error = "send sms: modem answered ERROR"`. Nothing in that line said which
step failed, and the device answered every command of the AT dialogue with `OK`:
`ATE0`, `AT+CMGF=1`, `AT+CSCS="GSM"`, even the `AT+CMGS="628…"` prompt. The
`ERROR` came from the message body submission (Ctrl-Z). The bodies were 178
characters — a Magic Link plus the copy around it:

```
Masuk ke Aplikasi Pembelian: http://…/masuk/tautan?token=… Berlaku 15 menit. Abaikan pesan ini bila Anda tidak memintanya.
```

Text mode (`AT+CMGF=1`) carries **one** segment: 160 septets of the GSM 7-bit
alphabet. There is no concatenation in text mode, so the modem refuses a longer
body. A 100-character body to the same number was accepted. The failure was
deterministic and message-specific, which is exactly what a retry budget cannot
fix: the drainer spent four attempts, and the log blamed the device.

## Decision

**1. The segment budget is a rule in the `sms` package, checked before a row
exists.** `sms.MaxSeptets` (160) and `sms.CheckText` count *septets*, not runes:
characters in the GSM extension table (`{}[]\|~^€\f`) cost two, so 160 characters
is not automatically 160 septets. A body that is too long, or that holds a
character outside the alphabet (an emoji, a typographic quote), is
`sms.Unsendable` and `Outbox.Enqueue` rejects it — the caller gets a cause it can
act on instead of a row that can never be delivered.

**2. The copy has a budget.** The Magic Link message is short enough for the whole
link inside one segment (`Tautan masuk: <link> (berlaku 15 menit). Abaikan bila
bukan Anda.`, ~147 septets with a production-length base URL). A test asserts the
composed SMS passes `sms.CheckText`, so a longer greeting, a longer `APP_BASE_URL`
or a longer TTL cannot silently reintroduce the bug. A base URL that does not fit
fails the request loudly instead of promising an SMS nobody will get.

**3. The adapter refuses an unsendable body before it opens the device, and names
the command it was rejected by.** `Send` checks the body first, so no port is
opened, no session is reopened, and no `ERROR` is guessed at. Every remaining
dialogue failure now reads `send sms: AT+CMGS body: modem answered ERROR` — the
line an operator needs at 3am.

**4. A rejected body leaves the modem at the `>` prompt, and that prompt outlives
the port.** Closing the device after a failure does not reset the modem: the next
session's `ATE0`/`AT+CMGF` were read back as *message text* during diagnosis.
`Send` now sends ESC to cancel the prompt before closing, and opening a device
sends ESC and drains the stale answer before the first command.

**5. "Unsendable" is final, not retryable.** The drainer closes such a row on the
first attempt (`MarkFailed` with no next attempt) and logs why, instead of
spending the attempt budget on a guaranteed failure and keeping the PIN or token
in Postgres for the whole backoff schedule (ADR 0016). This is also what clears
rows written before this rule existed.

## Alternatives rejected

- **PDU mode with concatenated SMS.** Correct for arbitrary length, but it needs
  a PDU encoder, 7-bit packing and UDH headers — a new serial protocol to replace
  a 160-septet budget that a link and a PIN fit in comfortably. Reconsider only if
  the product ever needs a multi-segment message (a receipt, a long URL).
- **Truncating the body to 160 characters.** Truncation lands in the middle of the
  Magic Link or the PIN: the only part that matters.
- **Leaving it to the retry budget.** That is the reported bug: 30+ minutes of
  backoff, an `ERROR` log that names no command, and a buyer who never receives
  the link while `Account Status` still shows the PIN.
- **Validating only in the composer.** It fixes today's message and leaves the
  next caller (provisioning, a future transactional SMS) with the same opaque
  `ERROR` from the device.

## Consequences

- Message copy is now budgeted. `sms.CheckText` is the review check for any new
  SMS text; the error names the limit and the count, never the body (it holds a
  PIN or a token).
- 160 *characters* is not 160 *septets*: one `€` or `{` costs two. Counting runes
  would let a "fit" message fail at the modem again.
- Rows already in `sms_outbox` with an over-long body are closed as failed on the
  next drain, so their `message` is emptied (ADR 0016). Those links are expired by
  then; a buyer asks for a new one.
- The adapter's ESC recovery is harmless on a healthy session (ESC at the AT
  prompt is ignored) and costs one quiet window (~200 ms) per device open, not per
  message.
