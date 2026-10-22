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

Rules:

- A fixture is a **complete** response body, envelope included.
- `checks.<name>.error` is a stable failure category (`timeout`, `unreachable`,
  `auth_failed`, `unavailable`), never a driver message; it is required when a
  check is `down`.
- Values are examples, not assertions of live data (latency, timestamps).
- Never edit a fixture to make one side's test pass without updating the other
  side's schema/DTO in the same change.
