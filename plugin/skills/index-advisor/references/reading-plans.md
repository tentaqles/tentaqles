# Reading query plans

## Postgres: EXPLAIN (ANALYZE, BUFFERS)

Read the tree bottom-up and inside-out; the deepest node runs first. Each node
shows `cost=startup..total` (planner estimate, arbitrary units), `rows=`
(estimated), and with ANALYZE `actual time=first..last rows=N loops=L`.
Multiply per-loop numbers by `loops`.

| Signal | Meaning | Usual fix |
|---|---|---|
| estimated `rows` vs `actual rows` off by 10x+ | stale or missing statistics, correlated columns | `ANALYZE table;` raise `default_statistics_target` for the column; `CREATE STATISTICS` for correlated columns |
| `Seq Scan` with `Rows Removed by Filter` >> rows returned | no usable index for the predicate | B-tree / partial index on the filter columns |
| `Index Scan` + large `Rows Removed by Filter` | index covers only part of the predicate | composite index; reorder columns |
| `Bitmap Heap Scan` with `Recheck Cond` and `lossy` blocks | `work_mem` too small for the bitmap | more selective index or more `work_mem` |
| `Sort` with `Sort Method: external merge  Disk:` | sort spilled to disk | index matching `ORDER BY` (column order and direction), or `work_mem` |
| `Hash` with `Batches: > 1` | hash join spilled | `work_mem`, or reduce rows earlier |
| `Nested Loop` with a big outer side and inner `Seq Scan` | missing index on the join column (often an FK) | index the inner join column |
| `Index Only Scan` with high `Heap Fetches` | visibility map is stale | `VACUUM` the table; check autovacuum |
| `shared read=` high, `shared hit=` low | data not cached, I/O bound | fewer pages touched (better index), more RAM |
| `Limit` over a big `Sort` | top-N without an index | index on the ORDER BY columns so the scan stops early |

Buffers: `shared hit` is pages found in cache, `read` is pages read from disk
or OS cache; one page is 8 kB. Compare total buffers before and after: an
index that cuts buffers by 100x is a win even if the dev timing looks equal.

Gotchas:

- A function or cast on the column (`WHERE lower(email) = ...`,
  `WHERE created_at::date = ...`) cannot use a plain index; use an expression
  index or rewrite the predicate as a range.
- `LIKE '%abc'` cannot use a B-tree; use `pg_trgm` + GIN.
- `OR` across different columns often falls back to a seq scan; consider
  `UNION ALL` or a bitmap-friendly pair of indexes.
- A parameterized query can get a generic plan; test with `EXPLAIN (ANALYZE)
  EXECUTE stmt(...)` after 5+ executions if the app uses prepared statements.
- Small tables: a seq scan is often correct. Do not "fix" it.
- Supabase: RLS predicates are added to every query on the table; index the
  columns policies filter on (`user_id`, `tenant_id`), and wrap `auth.uid()`
  in `(select auth.uid())` so it is evaluated once.

## SQL Server / Azure SQL: execution plans

Read right-to-left, top-to-bottom; arrow thickness is row count. Compare
"Estimated Number of Rows" with "Actual Number of Rows" on each operator.

| Operator / warning | Meaning | Usual fix |
|---|---|---|
| `Table Scan` (heap) / `Clustered Index Scan` | reads the whole table | nonclustered index on the predicate |
| `Index Seek` + `Key Lookup` (many rows) | index finds rows, then fetches missing columns one by one | add the columns as `INCLUDE (...)` |
| `Index Scan` on a nonclustered index | predicate not sargable or wrong leading column | fix column order; remove functions on columns |
| Yellow warning: implicit conversion | type mismatch (e.g. `nvarchar` param vs `varchar` column) kills seeks | match parameter types |
| `Sort` / `Hash Match` with spill warning | memory grant too small | supporting index, update statistics |
| Estimated vs actual off by 10x+ | stale stats or parameter sniffing | `UPDATE STATISTICS`; `OPTION (RECOMPILE)` / Query Store plan forcing for sniffing |
| Missing index hint (green text) | optimizer suggestion | evaluate it; it ignores write cost and existing indexes |

Use `SET STATISTICS IO ON` and compare `logical reads` before and after; it is
the SQL Server equivalent of buffers. Query Store (`sys.query_store_*`) shows
regressions and plan changes over time.
