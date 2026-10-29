---
name: db-migration
description: Plan, write and review a safe database schema change for Postgres (incl. Supabase) or SQL Server / Azure SQL using expand/contract, lock-safe DDL, up/down scripts and a rollback plan. Use when adding, dropping, renaming or retyping a column, table, constraint or index; writing or reviewing a migration file (raw SQL, Supabase, Prisma, Drizzle, Alembic, Flyway, EF Core, Django, Rails); backfilling data; or when the user says "migration", "schema change", "alter table", "add a column", "NOT NULL", "rename column" or worries a change will lock a big table.
---

# DB Migration

Ship schema changes that never lock a hot table for long, never break the
code that is running during the deploy, and can always be rolled back.

**Core rule:** a migration is a reviewed, versioned file run by the project's
migrator. Never run DDL by hand against a shared database (staging or prod).

## 1. Identify the engine and the migrator

Read the repo before writing anything: `supabase/migrations/`,
`prisma/schema.prisma`, `drizzle.config.*`, `alembic.ini`, `flyway.conf`,
`*.csproj` with EF Core, `db/migrate/`, `*.sqlproj` (DACPAC). Use that tool.
With an ORM, edit the schema, **generate** the migration, then **read the
generated SQL**: a generated migration is not automatically a safe one.

## 2. Classify every statement

For each statement decide: **safe** (metadata-only, instant), **locking**
(rewrites the table, scans it under a strong lock, or blocks writes), or
**destructive** (drops, renames, narrows a type, deletes data). Use the
per-engine tables in [references/ddl-safety.md](references/ddl-safety.md).

Anything locking or destructive on a table that is big or busy gets rewritten
as expand/contract.

## 3. Expand/contract (parallel change)

Never drop, rename or retype something in the same deploy as the code that
stops using it. Split the change across releases:

| Step | Migration | Code |
|---|---|---|
| 1. Expand | Add the new column/table **nullable**, no volatile default | Writes both old and new (dual-write) |
| 2. Backfill | Separate, idempotent job in batches (1k-10k rows, short transactions, resumable by key) | unchanged |
| 3. Enforce | `CHECK ... NOT VALID` then `VALIDATE`; `SET NOT NULL` once a validated check exists; unique via concurrent index | unchanged |
| 4. Switch reads | none | Reads the new shape; still dual-writes |
| 5. Contract | Drop the old column/table **in a later release**, once no deployed code touches it | Stops writing the old shape |

A rename is: add new, dual-write, backfill, switch reads, drop old. A type
change is the same with a new column. Keep big backfills out of schema
migrations.

With the tq db pack, a contract step (`DROP COLUMN`, `DROP TABLE`, a rename,
`sp_rename`) carries a marker on the line before it that names its expand step:
`-- tq:contract <expand migration or PR>`. Without it the lint reports `PG008`
(Postgres) or `MS005` (SQL Server).

## 4. Lock-safe DDL

- **Postgres:** start every migration session with `SET lock_timeout = '5s';`
  and a `statement_timeout`; retry on timeout instead of queueing behind a
  long transaction. `CREATE INDEX CONCURRENTLY` (outside a transaction block,
  so many migrators need a "no transaction" marker). Add FKs and CHECKs as
  `NOT VALID`, then `VALIDATE CONSTRAINT` in a separate step. Unique: `CREATE
  UNIQUE INDEX CONCURRENTLY`, then `ADD CONSTRAINT ... UNIQUE USING INDEX`.
- **SQL Server / Azure SQL:** `ONLINE = ON` (and `RESUMABLE = ON`) for index
  builds and rebuilds where the edition supports it, `WITH (WAIT_AT_LOW_PRIORITY
  ...)` to avoid lock queues, `SET LOCK_TIMEOUT`, `WITH NOCHECK` then `WITH
  CHECK CHECK CONSTRAINT` for constraints. Adding a NOT NULL column with a
  constant default is metadata-only on modern versions.
- **Supabase:** every new table in `public` gets `ENABLE ROW LEVEL SECURITY`
  plus policies **in the same migration**. Review `supabase db diff` output.

**Lint rule ids** (`db/migration-lint`, `tq pack check --pack db`, when the db
pack is enabled; tables created in the same diff are skipped):

| Postgres | SQL Server / Azure SQL | Rule |
|---|---|---|
| `PG001`, `PG010` | `MS001` | index without `CONCURRENTLY` / `ONLINE = ON`; `CONCURRENTLY` inside a transaction |
| `PG002`, `PG003` | `MS002` | `NOT NULL` without `DEFAULT`; volatile default |
| `PG004` | `MS003` | type change or shrink (a rewrite) |
| `PG005`, `PG006`, `PG007` | `MS004` | `SET NOT NULL` without a validated check; constraint without `NOT VALID` / `WITH NOCHECK`; unique not `USING INDEX` |
| `PG008` | `MS005` | drop or rename without `-- tq:contract` (§3) |
| `PG009` | `MS006` | locking statement with no lock timeout |
| `PG011` | `MS007` | `UPDATE`/`DELETE` without `WHERE` |
| `PG012` | `MS008` | `VACUUM FULL`, `CLUSTER`, `LOCK TABLE`, plain `REINDEX`; DDL in `BEGIN TRAN` across `GO` |

A deliberate exception is suppressed on the line before the statement, with a
reason: `-- tq:allow PG001: <reason>`. RLS rules (`RLS001`…) are `db/rls`; see
the `rls-tests` skill.

## 5. Up, down, and never edit history

- Every migration has an **up** and a **down** that reverses exactly the up
  (forward-only tools such as Prisma: write the reverse migration you would
  ship, and say so in the PR). A down that would lose data says so and is
  only run with a restore point.
- With the db pack, `db/migration-rollback` checks the partner (`*.down.sql`,
  `-- migrate:down`, Flyway `U<ver>__*.sql`, sqitch `revert/`, alembic
  `downgrade()`, EF Core `Down()`). Forward-only migrators (Supabase, Prisma,
  Drizzle, DACPAC) need a `### Rollback` subsection under the spec's
  `## Migration plan` instead.
- **Never edit a migration that was applied anywhere shared.** Write a new
  one. tq's guard asks on this (`tq/migration-edit`): treat that prompt as a
  stop sign, not a formality.
- Ad-hoc DDL through `psql`/`sqlcmd`/`supabase db query` triggers
  `tq/sql-destructive-shell`, and writes through a database MCP server trigger
  `tq/mcp-sql-write`. Move the statement into a migration file instead.
- Connection strings stay in env: `tq dotenv run -- npx prisma migrate deploy`
  or `tq dotenv run -- bash -c 'psql "$DATABASE_URL" -f up.sql'`. Never print
  the URL.

## 6. Test before shipping

Run **up -> down -> up** on a local container. Then run it on staging with
production-like **anonymized** data and time it: a statement that takes
seconds on 100 rows can lock for minutes on 10M. `EXPLAIN` the queries the
change targets (see the `index-advisor` skill for new indexes and FKs).

## 7. Pre-flight and rollback

Fill in the checklist and the rollback plan from
[references/preflight-rollback.md](references/preflight-rollback.md) and put
both in the PR description, including the deploy order (migrate before or
after the code). Run `tq decide triage --staged` on the change: schema,
migration and RLS diffs come back `high`, which means a full review.
With the db pack enabled, `tq pack crew` adds the `db-reviewer`, which judges
the rows of [references/review-checklist.md](references/review-checklist.md)
(`D1`…`D12`); use the same rows to review by hand.

## Red flags

- `ADD COLUMN ... NOT NULL` with no default on an existing table (Postgres).
- `CREATE INDEX` without `CONCURRENTLY` / `ONLINE = ON` on a live table.
- `DROP COLUMN` / `RENAME` in the same PR as the code change that stops using it.
- `UPDATE big_table SET ...` with no batching inside a schema migration.
- An edited file under an applied migrations directory.
- A new Supabase table with no RLS.
