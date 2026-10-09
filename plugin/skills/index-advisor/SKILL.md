---
name: index-advisor
description: Diagnose a slow query and recommend concrete index DDL for Postgres (incl. Supabase) or SQL Server / Azure SQL, with the write-cost tradeoff and how to verify it. Use when a query or endpoint is slow, when reading EXPLAIN / EXPLAIN ANALYZE output or a SQL Server execution plan, when adding foreign keys or new access patterns, when auditing for missing, unused or duplicate indexes, or when the user says "add an index", "seq scan", "table scan", "why is this query slow", "index this", or "pgvector index".
---

# Index Advisor

Every index must be justified by an access pattern: a real query, how often
it runs, and the table's read/write ratio. "It feels slow" is not a reason;
a plan is.

## 1. Get the facts

Ask for (or find in the code) the exact query with realistic parameters, how
often it runs, the table sizes, and the write rate. Then get the plan on
production-like data, never on an empty dev table:

- **Postgres:** `EXPLAIN (ANALYZE, BUFFERS, VERBOSE) <query>;`
  (`ANALYZE` executes the query: wrap writes in `BEGIN; ... ROLLBACK;`).
- **SQL Server:** actual execution plan (`SET STATISTICS XML ON;` or SSMS
  "Include Actual Execution Plan"), plus `SET STATISTICS IO, TIME ON;`.

Run read-only diagnostics through env, never a pasted connection string:
`tq dotenv run -- bash -c 'psql "$DATABASE_URL" -f explain.sql'`.

How to read the plan: [references/reading-plans.md](references/reading-plans.md).

## 2. Diagnose

Look for, in order: estimated vs actual rows off by 10x+ (stale statistics:
`ANALYZE` first, maybe no index needed), a Seq Scan / Table Scan or Index
Scan with a large `Rows Removed by Filter`, a Sort or Hash spilling to disk,
Nested Loop with a big inner side, Key Lookups (SQL Server) on many rows.

Also run the audit queries in
[references/audit-queries.md](references/audit-queries.md): foreign keys with
no index, unused indexes, duplicate or prefix-redundant indexes, and SQL
Server's missing-index DMVs (treat those as hints, not answers).

## 3. Design the index

- **Composite order:** equality columns first, then the range or sort column;
  the leftmost-prefix rule decides which queries it can serve. `(tenant_id,
  status, created_at)` serves `WHERE tenant_id=? AND status=? ORDER BY
  created_at`, and `WHERE tenant_id=?`, but not `WHERE status=?`.
- **Partial / filtered:** index only the hot subset (`WHERE deleted_at IS
  NULL`, `WHERE status = 'pending'`). The query must repeat the predicate.
- **Covering:** `INCLUDE (...)` (PG 11+, SQL Server) to get index-only scans
  or avoid key lookups; keep included columns small.
- **Foreign keys:** Postgres does not index the referencing column; index it
  when you join on it or delete parents (`ON DELETE CASCADE` scans the child).
- **Type:** B-tree for equality, range, sort and `LIKE 'abc%'`; GIN for
  `jsonb`, arrays, full text, `pg_trgm`; GiST for geo and ranges; BRIN for
  huge append-only tables ordered by time; pgvector HNSW (or IVFFlat) with the
  opclass matching the operator (`vector_cosine_ops` for `<=>`), plus a B-tree
  on the tenant/filter column used with it.
- **Never:** a second index on the primary key, an index that is a prefix of
  an existing one, or an index nobody's query uses.

## 4. Weigh the write cost

Each index slows every INSERT, every UPDATE of an indexed column (and loses
Postgres HOT updates), and adds storage, vacuum and backup time. Say it
explicitly: "+1 index on a table with N writes/s; expected read gain X ms on
a query run Y times/min." On write-heavy tables, prefer one composite index
that serves several queries over several single-column ones, and drop what
the new index makes redundant.

## 5. Output

Deliver, for each recommendation:

1. The access pattern it serves (query + frequency).
2. The DDL, lock-safe: `CREATE INDEX CONCURRENTLY ...` (Postgres, outside a
   transaction) or `... WITH (ONLINE = ON)` (SQL Server, where supported),
   inside a migration file. Use the `db-migration` skill to ship it; ad-hoc
   DDL through `psql`/`sqlcmd` triggers tq's `tq/sql-destructive-shell` ask.
3. Indexes it makes redundant (to drop later, after checking usage).
4. Write-cost note.
5. Verification: the same `EXPLAIN (ANALYZE, BUFFERS)` before and after, with
   the expected plan change (e.g. Seq Scan -> Index Scan, buffers 120k ->
   40), then `idx_scan` rising in `pg_stat_user_indexes` (or
   `sys.dm_db_index_usage_stats`) after a day of traffic.

Template:

```markdown
| # | Query / pattern | DDL | Replaces | Write cost | Verify |
|---|---|---|---|---|---|
| 1 | orders by tenant, newest first (~40/s) | `CREATE INDEX CONCURRENTLY orders_tenant_created_idx ON orders (tenant_id, created_at DESC);` | `orders_tenant_idx` | 1 index, ~200 inserts/s | Seq Scan -> Index Scan; p95 120ms -> <5ms |
```
