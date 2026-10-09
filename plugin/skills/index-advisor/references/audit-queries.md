# Index audit queries

All read-only. Run them on the production replica or a recent copy; usage
statistics on a dev database mean nothing. Statistics reset on restart,
failover or `pg_stat_reset()`, so check how long they have been collecting
before calling an index "unused".

## Postgres

### Foreign keys without a supporting index

```sql
SELECT c.conrelid::regclass AS table_name,
       c.conname            AS fk_name,
       string_agg(a.attname, ', ' ORDER BY x.n) AS fk_columns
FROM pg_constraint c
CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS x(attnum, n)
JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = x.attnum
WHERE c.contype = 'f'
  AND NOT EXISTS (
    SELECT 1 FROM pg_index i
    WHERE i.indrelid = c.conrelid
      -- FK columns must be the leading columns of some index
      AND (i.indkey::int2[])[0:cardinality(c.conkey) - 1] @> c.conkey
  )
GROUP BY c.conrelid, c.conname
ORDER BY 1, 2;
```

### Unused indexes

```sql
SELECT s.schemaname, s.relname AS table_name, s.indexrelname AS index_name,
       s.idx_scan,
       pg_size_pretty(pg_relation_size(s.indexrelid)) AS index_size
FROM pg_stat_user_indexes s
JOIN pg_index i ON i.indexrelid = s.indexrelid
WHERE s.idx_scan = 0
  AND NOT i.indisunique          -- unique/PK indexes enforce constraints
  AND NOT i.indisprimary
ORDER BY pg_relation_size(s.indexrelid) DESC;

-- how long have stats been collecting?
SELECT stats_reset FROM pg_stat_database WHERE datname = current_database();
```

On replicas, check usage on every node: an index unused on the primary may
serve read traffic on a replica.

### Duplicate and prefix-redundant indexes

```sql
-- exact duplicates (same table, columns, expressions, predicate)
SELECT indrelid::regclass AS table_name,
       array_agg(indexrelid::regclass) AS indexes
FROM pg_index
GROUP BY indrelid, indkey, indclass, coalesce(indexprs::text, ''), coalesce(indpred::text, '')
HAVING count(*) > 1;

-- index A is a leading prefix of index B (A is usually redundant unless unique)
SELECT a.indrelid::regclass AS table_name,
       a.indexrelid::regclass AS redundant_index,
       b.indexrelid::regclass AS covered_by
FROM pg_index a
JOIN pg_index b ON a.indrelid = b.indrelid AND a.indexrelid <> b.indexrelid
WHERE a.indpred IS NULL AND b.indpred IS NULL
  AND a.indexprs IS NULL AND b.indexprs IS NULL
  AND NOT a.indisunique
  AND array_length(a.indkey::int2[], 1) < array_length(b.indkey::int2[], 1)
  AND (b.indkey::int2[])[0:array_length(a.indkey::int2[], 1) - 1] = a.indkey::int2[];
```

### Tables that are mostly sequentially scanned

```sql
SELECT relname, seq_scan, seq_tup_read, idx_scan, n_live_tup
FROM pg_stat_user_tables
WHERE n_live_tup > 10000
ORDER BY seq_tup_read DESC
LIMIT 20;
```

### Slowest queries (needs pg_stat_statements)

```sql
SELECT calls, round(mean_exec_time::numeric, 1) AS mean_ms,
       round(total_exec_time::numeric) AS total_ms, rows, left(query, 200) AS query
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 20;
```

Supabase also exposes these through `supabase inspect db` (`unused-indexes`,
`index-usage`, `seq-scans`, `outliers`).

Dropping an index: `DROP INDEX CONCURRENTLY name;` in a migration, after
confirming it is unused everywhere for a full business cycle (month-end jobs
exist). Keep the `CREATE` statement in the down migration.

## SQL Server / Azure SQL

### Index usage (reads vs writes)

```sql
SELECT OBJECT_NAME(i.object_id) AS table_name, i.name AS index_name, i.type_desc,
       s.user_seeks, s.user_scans, s.user_lookups, s.user_updates
FROM sys.indexes i
LEFT JOIN sys.dm_db_index_usage_stats s
  ON s.object_id = i.object_id AND s.index_id = i.index_id AND s.database_id = DB_ID()
WHERE OBJECTPROPERTY(i.object_id, 'IsUserTable') = 1
  AND i.is_primary_key = 0 AND i.is_unique_constraint = 0
ORDER BY (ISNULL(s.user_seeks,0) + ISNULL(s.user_scans,0) + ISNULL(s.user_lookups,0)) ASC,
         s.user_updates DESC;
```

High `user_updates` with near-zero reads = pure write cost.

### Missing-index suggestions (hints only)

```sql
SELECT TOP 20
       d.statement AS table_name, d.equality_columns, d.inequality_columns, d.included_columns,
       s.user_seeks, s.avg_user_impact
FROM sys.dm_db_missing_index_details d
JOIN sys.dm_db_missing_index_groups g ON g.index_handle = d.index_handle
JOIN sys.dm_db_missing_index_group_stats s ON s.group_handle = g.index_group_handle
ORDER BY s.user_seeks * s.avg_user_impact DESC;
```

These suggestions ignore existing indexes and write cost and often overlap.
Merge them into one well-ordered index per access pattern.

### Foreign keys without an index

Flags FKs where no index has an FK column at the matching key position. For
multi-column FKs it is approximate: confirm the full column order by hand.

```sql
SELECT OBJECT_NAME(fk.parent_object_id) AS table_name, fk.name AS fk_name
FROM sys.foreign_keys fk
WHERE NOT EXISTS (
  SELECT 1
  FROM sys.foreign_key_columns fkc
  JOIN sys.index_columns ic
    ON ic.object_id = fkc.parent_object_id
   AND ic.column_id = fkc.parent_column_id
   AND ic.key_ordinal = fkc.constraint_column_id
  WHERE fkc.constraint_object_id = fk.object_id
);
```

Azure SQL also offers automatic tuning (`CREATE INDEX` / `DROP INDEX`
recommendations); review them the same way, through a migration.
