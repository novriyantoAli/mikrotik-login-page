# Seed the Package catalog until the Admin Panel exists

The **Login Page** reads the **Package** list from `GET /packages` (PRD point 8),
so the `packages` table must be populated before the page can show anything
real. The **Admin Panel** that will own catalog writes is out of scope for this
PRD, so nothing in the running system can create a **Package** yet.

Decision: the catalog starts from a starter list held in code
(`internal/application/packages/service/catalog.go`) and is written by a
dedicated `migrate` command that deployment runs once before the API starts —
a one-shot `migrate` service in compose, not a hook in the API process and not a
GORM `AutoMigrate` call on every boot. The command applies the schema
(`packages`, and later the other tables from PRD point 7) and then seeds the
starter catalog.

Each **Package** carries a stable `code` with a unique index, and the seed
inserts with `ON CONFLICT (code) DO NOTHING`. Consequences, all deliberate:

- Re-running the command never duplicates a **Package**, so a deployment can
  run it unconditionally. This is the idempotency the issue asks for.
- A row an operator edited by hand (there is no Admin Panel, so SQL is the only
  way to change a price today) survives every later deployment. The seed is a
  starting point, not a source of truth.
- Changing a starter value in code therefore does **not** update an existing
  row; it only affects deployments whose catalog does not have that `code` yet.
  Applying catalog changes is the **Admin Panel**'s job once it exists.
- The public `id` stays stable across deployments because the row is inserted
  once; the Login Page "Beli" URL carries that `id` to the purchase app.
- A **Package** deleted by hand is re-created by the next deployment, because
  the seed cannot tell a deletion from "never seeded".

Rejected alternatives:

- **Seed at API startup.** Every API replica would race the same inserts, a
  restart would silently mutate catalog data, and the process that answers
  public traffic would need write access to the catalog for no reason.
- **Seed from deployment configuration** (`config.yaml` / env). The catalog is
  domain data that will be edited in the **Admin Panel**, not a deployment knob;
  putting prices in env would create two competing sources and would make the
  `.env.example` key-coverage test the wrong place to describe products.
- **`ON CONFLICT DO UPDATE`.** It would keep the catalog equal to the code, but
  it would also overwrite an operator's price edit on every deploy — a data loss
  the operator cannot see coming while there is no Admin Panel.
- **A versioned migration tool now.** One table and one seed do not justify a
  tool; GORM `AutoMigrate` inside the same `migrate` command is enough and can
  be replaced later without changing the deployment contract (one command, run
  before the API).

The public response of `GET /packages` is display-oriented because the contract
is frozen by the Login Page (`{ id, nama, harga, durasiActivePeriod,
batasKecepatan }`, rendered verbatim). The entity therefore stores typed values
(`price_idr` as integer rupiah, the **Active Period** duration, the
`Mikrotik-Rate-Limit` value) and the DTO formats them; money never becomes a
float and provisioning later reads the typed columns, not the display strings.
