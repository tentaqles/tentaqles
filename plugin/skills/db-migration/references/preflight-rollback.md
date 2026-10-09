# Pre-flight checklist and rollback plan

Copy both sections into the PR description and fill them in.

## Pre-flight checklist

**The change**
- [ ] It is a versioned migration file run by the project's migrator, not ad-hoc SQL.
- [ ] Generated ORM SQL was read and matches the intent.
- [ ] No already-applied migration was edited (if `tq/migration-edit` asked, the answer was "new file").
- [ ] Every statement is classified safe / locking / destructive (see `ddl-safety.md`).
- [ ] Locking or destructive steps on large/busy tables are split into expand/contract releases.
- [ ] `lock_timeout` (PG) / `LOCK_TIMEOUT` (SQL Server) is set for the session.
- [ ] Indexes are built `CONCURRENTLY` / `ONLINE = ON`; constraints added `NOT VALID` / `WITH NOCHECK` then validated.
- [ ] Large data changes are a separate, batched, idempotent job, not part of the schema migration.
- [ ] New FK columns have an index; new indexes are justified by a query.
- [ ] Supabase: new tables have RLS enabled and policies in the same migration.

**Compatibility**
- [ ] The code currently deployed keeps working after the migration runs (no drop/rename/narrowing of anything it reads or writes).
- [ ] The new code works before and after the backfill finishes.
- [ ] Deploy order is written down: migrate-then-deploy (expand) or deploy-then-migrate (contract).

**Testing**
- [ ] up -> down -> up passed on a local database.
- [ ] Ran on staging with production-like, anonymized data; duration and lock waits recorded.
- [ ] `EXPLAIN` of the affected queries checked before and after.
- [ ] Row counts / checksums to verify the backfill are prepared.

**Operations**
- [ ] Backup or point-in-time restore point confirmed for destructive steps.
- [ ] Run window chosen for anything that cannot be made online.
- [ ] Someone can watch locks during the run (`pg_stat_activity` / `pg_locks`; `sys.dm_exec_requests` / `sys.dm_tran_locks`).
- [ ] Secrets come from env (`tq dotenv run -- <migrate command>`), nothing printed.

## Rollback plan

```markdown
### Rollback plan

**Migration:** <id / filename>
**Type:** expand | backfill | enforce | contract
**Reversible without data loss:** yes | no (why)

**Trigger to roll back:** <e.g. lock wait > 30s, error rate > X%, migration exceeds N minutes>

**Steps**
1. Stop the run: cancel the session (`SELECT pg_cancel_backend(<pid>)` / `KILL <spid>`).
   With lock_timeout set, a stuck DDL fails on its own and the transaction rolls back.
2. Roll back the code first if the new code depends on the new schema.
3. Apply the down migration: <command>.
4. For a contract step (data already dropped): restore from <backup / PITR timestamp>
   into a side table and copy the column back; never restore the whole database over live writes.
5. Verify: <query / row counts / health check>.

**Who decides:** <name/role>    **Expected time to restore:** <minutes>
```

Notes:

- An expand step is almost always safe to leave in place; rolling back the
  code is usually enough.
- A contract step is the dangerous one. Only run it once the previous release
  has been stable long enough that you will not need to go back to it.
- If a `CREATE INDEX CONCURRENTLY` fails, it leaves an `INVALID` index:
  `DROP INDEX CONCURRENTLY` it before retrying.
