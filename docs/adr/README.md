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
| [0013](0013-purchase-app-spa-single-origin.md) | Purchase app is a SPA; no SSR holds the session |

**Reserved:** 0010-0012 are the three remaining PRD decisions not yet written —
SMS outbox and modem serialisation, Magic Link/session lifetime details, and
seeding the Package catalog until the Admin Panel exists. They are numbered
ahead of time so parallel slices do not reuse a number.
