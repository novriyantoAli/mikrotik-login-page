# Contract fixtures

One shared set of example payloads, imported by both sides of every contract:

- the backend integration tests (`backend/test/integration`) unmarshal each
  fixture through its DTO and re-marshal it, so a field the DTO does not model
  (or a DTO field the fixture forgot) fails the build's tests;
- the purchase app tests (`frontend/tests`) parse each fixture through the
  matching zod schema, which is strict, so a missing field or an unknown field
  fails there too.

Changing a contract therefore means changing the fixture, and both suites go red
until they agree again. That is the point: the two sides cannot drift apart
silently.

| Fixture | Endpoint | Consumers |
| --- | --- | --- |
| `health.response.json` | `GET /api/health` (200, every dependency up) | `backend/test/integration/health_contract_test.go`, `frontend/tests/unit/health.contract.test.ts` |
| `health.degraded.response.json` | `GET /api/health` (503, one dependency down) | same two test files |
| `packages.response.json` | `GET /api/packages` (200) | `backend/test/integration/packages_contract_test.go`, `tests/unit/login-page-contract.test.js` |
| `active-period.response.json` | `GET /api/active-period?phone=…` (200) | `backend/test/integration/active_period_contract_test.go`, `tests/unit/login-page-contract.test.js` |
| `magic-link.request.json` | `POST /api/auth/magic-link` (request body) | `backend/test/integration/auth_contract_test.go`, `frontend/tests/unit/auth.contract.test.ts` |
| `magic-link.response.json` | `POST /api/auth/magic-link` (202) | same two test files |
| `magic-link.exchange.response.json` | `POST /api/auth/magic-link/exchange` (200) | same two test files |
| `session.response.json` | `GET /api/auth/session` (200) | same two test files |
| `channels.response.json` | `GET /api/transactions/channels` (200) | `backend/test/integration/transaction_contract_test.go`, `frontend/tests/unit/checkout.contract.test.ts` |
| `transaction.create.request.json` | `POST /api/transactions` (request body) | same two test files |
| `transaction.response.json` | `POST /api/transactions` and `GET /api/transactions/{id}` (200, pending) | same two test files |
| `transaction.paid.response.json` | `GET /api/transactions/{id}` (200, paid) | same two test files |
| `transaction.provisioned.response.json` | `GET /api/transactions/{id}` (200, provisioned, with the PIN) | same two test files |
| `account.response.json` | `GET /api/account` (200, summary with the current PIN) | `backend/test/integration/account_contract_test.go`, `frontend/tests/unit/account.contract.test.ts` |
| `account.extend-preview.response.json` | `GET /api/account/extend-preview?package_id=` (200) | same two test files |
| `transactions.response.json` | `GET /api/transactions?page=&page_size=` (200, history without the PIN) | same two test files |
| `admin-packages.response.json` | `GET /api/admin/packages` (200, typed values with `version`) | `backend/test/integration/admin_packages_test.go`; the panel's zod schema in `admin/tests` |
| `admin-login.request.json` | `POST /api/admin/auth/login` (request body) | `backend/test/integration/admin_contract_test.go`, `admin/tests/unit/auth.contract.test.ts` |
| `admin-session.response.json` | `POST /api/admin/auth/login` and `GET /api/admin/auth/session` (200) | same two test files |
| `admin-admins.response.json` | `GET /api/admin/admins` (200, one row per role) | `backend/test/integration/admin_contract_test.go`, `admin/tests/unit/admins.contract.test.ts` |
| `admin-package-create.request.json` | `POST /api/admin/packages` (request body) | `backend/test/integration/admin_contract_test.go`, `admin/tests/unit/packages.contract.test.ts` |
| `admin-package-update.request.json` | `PUT /api/admin/packages/{id}` (request body) | same two test files |
| `admin-transactions.response.json` | `GET /api/admin/transactions` (200, one page of the panel's search) | `backend/test/integration/admin_transactions_test.go`, `admin/tests/unit/transactions.contract.test.ts` |

Rules:

- A fixture is a **complete** response body, envelope included.
- `checks.<name>.error` is a stable failure category (`timeout`, `unreachable`,
  `auth_failed`, `unavailable`), never a driver message; it is required when a
  check is `down`.
- The Package and Active Period fixtures are the **frozen Login Page contract**
  (PRD point 8): `{ id, nama, harga, durasiActivePeriod, batasKecepatan }` and
  `{ expiry }`. The Login Page is finished, so these fixtures pin its shape and
  are consumed by the Login Page's own client seam (`tests/unit`). A deliberate
  change here is a breaking change to a shipped page, not a refactor. An
  unknown Phone Number answers `{"data":{"expiry":null}}`, which the seam
  renders as "no Active Period" rather than as a failure.
- The checkout fixtures are the **transaction contract** (ADR 0015/0016). The amount
  is integer rupiah, the channel fee is separate from it, and `paid_at` is
  `null` until settlement; the payload never carries a vendor field such as
  Tripay's own `reference`. `phone_number` is the buyer's own identity (the
  hotspot username) and `pin` is `null` until the **Transaction** is
  `provisioned`; the PIN never appears in any list response, and no fixture may
  grow a field that could carry a session token or a ciphertext.
- The auth fixtures are the **session contract** (ADR 0011). The session token
  never appears in a body — only in the `Set-Cookie` header — so a fixture that
  grows a `token` field is a leak, not a feature.
- The Account Status fixtures are the **Account Status contract** (ADR 0017).
  `pin` appears only in `account.response.json`; a `pin` field in
  `transactions.response.json` is a leak, not a feature, and `active_period.state`
  is one of `active`, `expiring`, `expired` or `none` computed by the backend.
- The admin fixtures are the **panel contract** (ADR 0024/0025/0028). The
  session body carries no token (the cookie is the session), and a fixture that
  grows one is a leak, not a feature. `is_active` and `version` are on every
  Package row so an edit can present the version it read; `role` is one of
  `owner`, `support`, `auditor`. The two Package write bodies are asymmetric on
  purpose: `code` is the seed's identity, so it is required on create and only
  echoed on edit (a rename is refused, ADR 0025 §5), and every edit carries the
  `version` it was based on.
- The admin Transaction fixture is the **panel's search contract** (PRD #35).
  It is paginated in the shared `{data, total_count, page, page_size}` shape,
  `package_name`/`amount_idr` are the snapshot the buyer paid for (ADR 0026),
  `last_sms_state` is the newest `sms_outbox` row (or `null` when nothing was
  queued), and there is **no `pin` field**: opening one is a separate, audited
  act, so a list response that grows one is a leak, not a feature.
- The admin PIN reveal fixture is the **panel's one credential contract**
  (ADR 0027 §4). `data.pin` is the whole body — six digits, one field — and it
  is the only admin fixture allowed to carry a credential: a PIN anywhere else,
  in a list or in this file's future siblings, is a leak. The endpoint is
  audited on every attempt, so this fixture describes a *successful* reveal;
  the outcome of a refusal is an error envelope, not a `pin` that is `null`.
- Values are examples, not assertions of live data (latency, timestamps).
- Never edit a fixture to make one side's test pass without updating the other
  side's schema/DTO in the same change.
