# Architecture Decision Records

Decisions are numbered in the order they were taken. The Login Page decisions
(0001-0006) predate the backend and purchase app; 0007 starts the monorepo
sequence.

| ADR | Decision |
| --- | --- |
| [0001](0001-freeradius-direct-db-provisioning.md) | Provision hotspot credentials by writing to FreeRADIUS directly |
| [0002](0002-separate-mysql-postgres.md) | Keep FreeRADIUS on MySQL, application data on Postgres |
| [0003](0003-sms-via-wavecom-modem.md) | Send SMS through a Wavecom modem |
| [0004](0004-phone-pin-credentials.md) | Phone Number + PIN credentials |
| [0005](0005-active-period-endpoint.md) | Public Active Period endpoint |
| [0006](0006-three-hotspot-login-modes.md) | Three hotspot login modes |
| [0007](0007-monorepo-new-build.md) | New monorepo for backend and purchase app |
| [0008](0008-tripay-payment-gateway.md) | Tripay behind a PaymentGateway port |
| [0009](0009-single-origin-opaque-session.md) | Single origin with an opaque session cookie |
| [0010](0010-sms-outbox-modem-serialisation.md) | SMS leaves through a Postgres outbox drained by the single modem-owning worker |
| [0011](0011-magic-link-session-lifetime.md) | Magic Link is a hashed single-use token; the session is a hashed opaque cookie |
| [0012](0012-seed-package-catalog.md) | Seed the Package catalog until the Admin Panel exists |
| [0013](0013-purchase-app-spa-single-origin.md) | Purchase app is a SPA; no SSR holds the session |
| [0014](0014-active-period-read-port.md) | Read the Active Period through the provisioning port; unknown means empty |
| [0015](0015-transaction-transition-guard.md) | Transaction transitions are guarded UPDATEs; checkout idempotency is a partial unique index |
| [0016](0016-pin-at-rest-and-idempotent-provisioning.md) | PIN is versioned AES-GCM ciphertext on the Transaction; provisioning inputs are persisted once so retries cannot extend the Active Period or rotate the PIN |

0010 and 0011 were **reserved** (not written) while 0012–0014 were already in
place, so the numbers record the order the PRD decisions were taken, not the
order the files were created. No numbers are free: the next decision continues
at 0017.
