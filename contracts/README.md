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
- Values are examples, not assertions of live data (latency, timestamps).
- Never edit a fixture to make one side's test pass without updating the other
  side's schema/DTO in the same change.
