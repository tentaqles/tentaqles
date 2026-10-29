# DB: restore drills

A backup nobody has restored is a hope. The db pack restores a declared backup
into a local scratch database on demand, records the evidence, and checks that
the newest drill is fresh when migrations change (spec D §15).

## Config

```yaml
packs:
  db:
    test_db: {url_env: TEST_DATABASE_URL}   # the local server the drill restores into
    drill:
      - name: main
        engine: postgres                     # postgres | sqlserver
        source: {kind: file, path_env: BACKUP_DUMP_PATH}
        verify: [public.orders, public.customers]
        freshness: 30d
```

- **Source kinds:** `file` (a dump or backup at the path in `path_env`),
  `supabase-dump`, `azure-sql-bacpac` and `command` (a command that writes the
  dump). A backup's location is given as a variable name (`path_env`), never
  as a literal secret.
- `drill` is trusted-layer only: its source names what tq reads, and a
  `command` source is run.

## Running a drill

`tq pack db drill <name> [--keep]` (the `restore-drill` skill):

1. reads the backup read-only;
2. restores it into a scratch database `tq_drill_<name>_<ts>` on the local
   `test_db` server, and refuses any target that is not local;
3. records the row counts of the `verify` tables, the schema size and the
   migration version as one line in `docs/ops/restore-drills.jsonl`;
4. drops the scratch database. `--keep` keeps the dump files under the db
   pack's state directory, never in the repository.

Only names and numbers are recorded: `host_kind`, `backup_taken_at`,
`restored_at`, `migration_version`, `tool_versions` and the counts, never row
content, a host or a connection string. Commit the line.

## Check and freshness

| Check | Kind | What it does |
|---|---|---|
| `db/restore-drill` | built-in | when a drill is declared and a migration changed: the newest `ok` line per drill is within its `freshness` |

`tq pack db approve --env <env>` binds the newest drill line into the
approval's evidence and is refused when a declared drill is stale (a human may
pass `--no-drill`). Recording a new drill voids a pending migration approval.
`tq pack doctor` reports `db-drill-stale`, `db-drill-invalid` and
`db-drill-log-unreadable`, and `tq insights` shows each drill's age (the
`drill_age_days:<name>` gauge, its latest value).

## The restore guard

`tq/restore-target` (deny) blocks an agent's restore into a declared
environment or a host that is not local: `pg_restore -d`, `psql -f` or
`psql < dump`, a pipe into `psql`, `sqlpackage /a:Import`, `RESTORE DATABASE`
through sqlcmd or Invoke-Sqlcmd, and `supabase db reset --linked` or
`--db-url`. A restore inside a payload tq cannot read (a container or remote
shell, a script) is denied too. Restore only into a local server named on the
command line, or use `tq pack db drill`.
