# RouterOS status page reads the Active Period from a public backend endpoint

The RouterOS hotspot status page must show the user's remaining **Active Period**. RouterOS template variables expose session state (`uptime`, `session-time-left`), not the FreeRADIUS `Expiration` that defines the Active Period, so the page cannot derive it locally.

Decision: the status page reads `GET <backendBaseUrl><activePeriodPath>?phone=<Phone Number>`, which returns `{ expiry }` — an ISO-8601 timestamp or epoch seconds for the end of the Active Period. `activePeriodPath` is deployment config (default `/active-period`), read by the client seam like the Package list endpoint. The request is read-only; the backend (out of scope for this repo) implements it.

Rejected alternatives: showing `session-time-left` (it is remaining *session* time, not the Active Period, and resets per session); scraping the legacy Mikhmon expiry iframe (unrelated external service).
