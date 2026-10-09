# DDL safety by engine

"Lock" here means the lock a statement holds and for how long. A short
`ACCESS EXCLUSIVE` lock is fine; one held while the table is rewritten or
scanned is an outage on a busy table. A statement waiting for a lock also
blocks every query queued behind it, which is why `lock_timeout` matters even
for "instant" DDL.

## Postgres (11+) and Supabase

| Operation | Risk | Safe pattern |
|---|---|---|
| `ADD COLUMN` nullable, no default | safe (metadata) | as is |
| `ADD COLUMN ... DEFAULT <constant>` | safe on PG 11+ (metadata) | as is |
| `ADD COLUMN ... DEFAULT <volatile>` (`now()`, `gen_random_uuid()`) | rewrites table | add nullable, backfill in batches, then set default for new rows |
| `ADD COLUMN ... NOT NULL` without default | fails on non-empty table / needs rewrite | add nullable, backfill, enforce (below) |
| `ALTER COLUMN ... SET NOT NULL` | full scan under `ACCESS EXCLUSIVE` | `ADD CONSTRAINT c CHECK (col IS NOT NULL) NOT VALID;` -> `VALIDATE CONSTRAINT c;` -> `SET NOT NULL` (PG 12+ skips the scan) -> drop the check |
| `ALTER COLUMN ... TYPE` | usually rewrites table + rebuilds indexes | new column + dual-write + backfill + switch (expand/contract). Safe exceptions: `varchar(n)` -> larger `n` / `text` |
| `CREATE INDEX` | blocks writes for the whole build | `CREATE INDEX CONCURRENTLY` (not in a transaction; check for an `INVALID` index on failure and drop it) |
| `DROP INDEX` | `ACCESS EXCLUSIVE` | `DROP INDEX CONCURRENTLY` |
| `ADD FOREIGN KEY` | scans + locks both tables | `ADD CONSTRAINT fk ... NOT VALID;` then `VALIDATE CONSTRAINT fk;` in a later statement. Index the FK column (concurrently) first |
| `ADD CHECK` | full scan under lock | `NOT VALID` + `VALIDATE CONSTRAINT` |
| `ADD UNIQUE` / `PRIMARY KEY` | builds index under lock | `CREATE UNIQUE INDEX CONCURRENTLY idx ...;` then `ADD CONSTRAINT u UNIQUE USING INDEX idx;` |
| `RENAME COLUMN` / `RENAME TABLE` | instant, but breaks running code | expand/contract; or a view/compat column for one release |
| `DROP COLUMN` | instant, breaks running code, loses data | contract step only, after code stopped using it; ORMs that `SELECT *` or cache columns need a deploy first |
| `DROP TABLE` / `TRUNCATE` | destructive | contract step, backup/restore point first |
| `VACUUM FULL`, `CLUSTER` | rewrites under `ACCESS EXCLUSIVE` | `pg_repack` or schedule a window |
| Large `UPDATE` / `DELETE` | long transaction, bloat, replica lag | batched job keyed on PK, `LIMIT`ed, committed per batch, resumable |
| Enum: `ALTER TYPE ... ADD VALUE` | fine, but not usable in the same transaction (PG < 12) | separate migration; removing values = new type |

Session settings for every migration:

```sql
SET lock_timeout = '5s';        -- fail fast instead of queueing behind a long tx
SET statement_timeout = '15min'; -- upper bound; raise only for known long steps
```

Supabase specifics:

- Migrations live in `supabase/migrations/<timestamp>_name.sql`; generate with
  `supabase migration new` or `supabase db diff`, read the diff, apply with
  the CLI or the pipeline, never through the dashboard SQL editor on prod.
- New table in an exposed schema: `ALTER TABLE ... ENABLE ROW LEVEL SECURITY;`
  and at least one policy in the same file. A table with RLS on and no policy
  is closed (safe); RLS off is open to the anon key.
- `SECURITY DEFINER` functions: set `search_path`, keep them out of exposed
  schemas unless intended.

Batched backfill skeleton (idempotent, resumable):

```sql
-- run repeatedly from a job until it updates 0 rows
WITH batch AS (
  SELECT id FROM orders
  WHERE total_cents IS NULL
  ORDER BY id
  LIMIT 5000
  FOR UPDATE SKIP LOCKED
)
UPDATE orders o
SET total_cents = (o.total * 100)::bigint
FROM batch
WHERE o.id = batch.id;
```

## SQL Server / Azure SQL

| Operation | Risk | Safe pattern |
|---|---|---|
| `ADD` nullable column | safe (metadata) | as is |
| `ADD ... NOT NULL DEFAULT <constant>` | metadata-only (2012+ Enterprise, Azure SQL) | as is; on Standard editions check, it may be size-of-data |
| `ALTER COLUMN` type / nullability | size-of-data, `Sch-M` lock | new column + backfill + switch; or `ALTER COLUMN ... WITH (ONLINE = ON)` where supported |
| `CREATE INDEX` | offline build blocks writes | `WITH (ONLINE = ON, RESUMABLE = ON, MAXDOP = n)` on Enterprise / Azure SQL; otherwise a window |
| Index rebuild | offline by default | `ALTER INDEX ... REBUILD WITH (ONLINE = ON, WAIT_AT_LOW_PRIORITY (MAX_DURATION = 5 MINUTES, ABORT_AFTER_WAIT = SELF))` |
| `ADD CONSTRAINT` FK / CHECK | scans under lock | `WITH NOCHECK ADD CONSTRAINT ...` then `ALTER TABLE ... WITH CHECK CHECK CONSTRAINT ...` (a `NOCHECK` constraint is untrusted and the optimizer ignores it until checked) |
| `sp_rename` | breaks running code, invalidates plans | expand/contract or a synonym/view for one release |
| `DROP COLUMN` | metadata, but breaks code; space not reclaimed until rebuild | contract step only |
| Large `UPDATE` / `DELETE` | log growth, lock escalation at ~5000 locks | `WHILE` loop with `TOP (n)`, commit per batch, keep batches under the escalation threshold |

Session settings:

```sql
SET LOCK_TIMEOUT 5000;     -- ms
SET XACT_ABORT ON;         -- a failed statement rolls the whole batch back
```

Tooling: EF Core migrations (`dotnet ef migrations script --idempotent` for
review), DbUp, Flyway, or SSDT/DACPAC with `sqlpackage /Action:Script` to
review the generated script before `/Action:Publish`. Set
`BlockOnPossibleDataLoss=True` in DACPAC publishes. Never apply changes ad hoc
from SSMS to production.
