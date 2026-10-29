# Pruning and plan reading, per engine

What the estimate's number means, and what usually brings it down. Propose the
change; measure again with `tq pack data cost estimate`; report before and after.

## BigQuery

- **Number:** `total_bytes_processed` from the dry run. On-demand billing charges for
  bytes processed, so it is the cost (capped per query by `--maximum_bytes_billed`).
- **Pruning:** partitioned tables skip partitions only when the filter on the
  partition column is a constant expression or a query parameter. A filter through a
  function of the column, or through a join, may scan every partition. Clustered
  columns prune blocks inside a partition when they appear in the filter.
- **Columns:** BigQuery is columnar; every selected column adds its full width.
  `SELECT *` on a wide table is the most common overrun. `LIMIT` does not reduce bytes.
- **Fixes:** list columns; filter on the partition column; set
  `require_partition_filter` on large tables (a human/DDL change); materialise a
  narrow intermediate table for repeated reads.

## Snowflake

- **Number:** `bytes_assigned` (GlobalStats), with `partitions_assigned` of
  `partitions_total`. EXPLAIN compiles only; it does not need a running warehouse but
  uses cloud-services credits.
- **Pruning:** micro-partition pruning works on min/max metadata. A selective filter
  on the clustering key (or a naturally ordered column such as a load date) assigns
  few partitions. Casting or wrapping the column in a function defeats it.
- **Compute:** cost is warehouse size times running time. A larger warehouse rarely
  helps a scan-bound query; first prune, then size.

## Databricks SQL

- **Number:** the largest `sizeInBytes` in the optimized logical plan of
  `EXPLAIN COST` (the scans), not the root (the result). Spark reports `8.0 EiB` when a
  source has no statistics: the estimate then says unknown.
- **Pruning:** partition filters, data skipping on Z-ordered or liquid clustered
  columns, and file statistics. Fresh statistics (`ANALYZE TABLE ... COMPUTE
  STATISTICS`, run by the pipeline or the human) make the estimate meaningful.
- **Compute:** SQL warehouse size and cluster count, or the job cluster's workers
  (`max_workers` cap).

## Synapse dedicated SQL pool

- **Number:** `total_cost` of `<dsql_operations>` (estimated time in ms) and
  `total_number_operations`. EXPLAIN does not run the statement.
- **Data movement:** `SHUFFLE_MOVE` and `BROADCAST_MOVE` operations dominate. Join and
  aggregate on the distribution column; replicate small dimension tables.
- **Compute:** the pool's performance level (`max_dwu`); Spark pools by node count
  (`max_spark_nodes`).

## Fabric warehouse

- **Number:** `StatementSubTreeCost` of the first statement in the SHOWPLAN_XML plan.
  It is a relative optimizer cost: compare the before and after of a change.
- **Compute:** capacity SKU (`max_capacity_sku`); a heavy query consumes capacity units
  shared with every other workload on the capacity.

## Postgres (as a warehouse)

- **Number:** `Total Cost` of the root plan node of `EXPLAIN (FORMAT JSON)`, in
  planner units (`max_plan_cost`). EXPLAIN without ANALYZE does not run the query.
- **Fixes:** an index on the filter or join column, a narrower select list, partition
  pruning on declaratively partitioned tables, and a `statement_timeout` on every
  ad-hoc query.
