# Migration review checklist (db pack)

Used by the `db-reviewer` agent and by `db-migration` §7. Judge each row from
the diff, the migrations it touches, the `## Migration plan` in the spec and the
`db/*` results of `tq pack check --pack db`. Cite file:line. Rows that need
table sizes, live traffic or a human decision are `U` rows. Row ids are stable:
reviewers copy them into their answers.

Severity guide: **BLOCKER** = a lock on a hot table with no timeout, data
loss, code that breaks during the deploy, rows exposed to the wrong user;
**MAJOR** = a control missing (no rollback, no pgTAP file for an exposed table,
an unbatched backfill); **MINOR** = hardening and performance hints.

## Answer-key rows (used by the db-reviewer agent)

| # | Check | Rule ids and source |
|---|---|---|
| D1 | Indexes on existing tables are built online: `CREATE INDEX CONCURRENTLY` outside a transaction (PG), `ONLINE = ON` (SQL Server, where the edition allows it) | `PG001`, `PG010`, `MS001` |
| D2 | Columns are added without a rewrite or a long lock: no `NOT NULL` without `DEFAULT`, no volatile default, no type change except the safe widenings | `PG002`, `PG003`, `PG004`, `MS002`, `MS003` |
| D3 | Constraints are added in two steps: `NOT VALID` then `VALIDATE` (PG), `WITH NOCHECK` then `CHECK CONSTRAINT` (SQL Server); `SET NOT NULL` only after a validated check; unique and primary keys `USING INDEX` | `PG005`, `PG006`, `PG007`, `MS004` |
| D4 | Every file with a locking statement sets a lock timeout (`SET lock_timeout`, `SET LOCK_TIMEOUT`); no `VACUUM FULL`, `CLUSTER`, `LOCK TABLE` or plain `REINDEX`; no DDL in a `BEGIN TRAN` that spans `GO` | `PG009`, `PG012`, `MS006`, `MS008` |
| D5 | Expand and contract across releases: drops, renames and narrowing ship after the code stops using them and carry `-- tq:contract <expand ref>`; the deployed code keeps working while the migration runs | `PG008`, `MS005`; `db-migration` §3 |
| D6 | Rollback: a down or undo partner for paired migrators, or `### Rollback` under `## Migration plan` for forward-only ones; a down that loses data says so and names the restore point | `db/migration-rollback`; `references/preflight-rollback.md` |
| D7 | Backfills are separate, batched, idempotent and resumable jobs; no `UPDATE` or `DELETE` without `WHERE` in a schema migration | `PG011`, `MS007` |
| D8 | Every new index is justified by a query, and every new foreign key column has an index (`index-advisor`) | `index-advisor` skill |
| D9 | RLS: every new table in an exposed schema enables RLS with policies per command in the same migration; no RLS disabled or policy dropped without a replacement; no `USING (true)` write policy for `anon` or `authenticated`; no policy on user-editable metadata | `RLS001`, `RLS002`, `RLS003`, `RLS004`, `RLS005`, `RLS009`, `RLS010` |
| D10 | Grants and bypasses: no write grant to `anon`, no `GRANT … TO public` in an exposed schema; views use `security_invoker = true`; `SECURITY DEFINER` functions set `search_path` and are not executable by `anon` without a reason | `RLS006`, `RLS007`, `RLS008` |
| D11 | pgTAP coverage: every exposed table with RLS has a test file that names it, using synthetic users only; the tests ran against a local database | `db/rls-tests`, `db/pgtap`; `rls-tests` skill |
| D12 | Deploy order and recovery: the migrate-before or deploy-before order is written down and matches the code; for destructive or risky changes the newest restore drill is within its freshness | `db/restore-drill`; `restore-drill` skill |

## Report format

```markdown
| # | Severity | Finding [row, rule] | Location | Fix |
|---|---|---|---|---|
| 1 | BLOCKER | [D2, PG002] NOT NULL column without a default on public.orders | supabase/migrations/20261028_add_status.sql:4 | add it nullable, backfill, CHECK … NOT VALID, VALIDATE, then SET NOT NULL |
```
