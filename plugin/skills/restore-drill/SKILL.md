---
name: restore-drill
description: "Prove a database backup restores: run tq pack db drill for a declared drill (Postgres/Supabase or SQL Server/Azure SQL), which restores the dump into a scratch database on the local test server, records row counts and the migration version as one evidence line in docs/ops/restore-drills.jsonl, and drops the scratch database. Explains each source kind (file, supabase-dump, azure-sql-bacpac, command) and keeps db/restore-drill fresh. Use when the user says \"test the backup\", \"restore drill\", \"can we restore\", \"backup freshness\", when db/restore-drill fails as stale, or when kickoff plans the first drill."
argument-hint: "<drill name> [--keep]"
---

# Restore drill

A backup that was never restored is a hope, not a backup. The db pack proves
it on a schedule: `packs.db.drill` declares each drill, `tq pack db drill
<name>` runs it, and the `db/restore-drill` check fails the definition of done
when a migration changed and the newest successful drill is older than its
`freshness`, or restored a backup that was already older than that.

The drill reads the backup and writes only to a scratch database on the local
`test_db` server. It never writes to a database that matches a declared
environment, and the `tq/restore-target` guard denies a `pg_restore`, `psql -f
<dump>`, `sqlpackage /a:Import`, `RESTORE DATABASE` or `supabase db reset
--linked` aimed at a declared environment or a non-local host.

## 1. Pick the drill

`packs.db.drill[]` entries have a `name`, an `engine`, a `source`, the
`verify` tables whose row counts are recorded, and a `freshness` (for example
`30d`). The manifest snippet is `templates/packs/db/packs-db.yaml` in the
plugin. The source is read-only:

| `source.kind` | What tq restores | Notes |
|---|---|---|
| `file` | the dump path named by the variable in `path_env` | a custom-format dump goes through `pg_restore --no-owner --no-acl`, a plain SQL dump through `psql -f`; a SQL Server `.bak` through `RESTORE DATABASE … WITH MOVE` on a local container |
| `supabase-dump` | `supabase db dump` from the linked project | the linked ref must match a declared environment. A plain `supabase db dump` is schema only: data needs a second dump with `--data-only` (and roles `--role-only`), restored with `psql -f` in the order roles, schema, data |
| `azure-sql-bacpac` | an exported `.bacpac` | restored with `sqlpackage /a:Import` into the local SQL Server |
| `command` | the project's own fetch command, with a timeout | the command prints or writes the dump path; it must only read |

The dump lands in the db pack's state directory and is deleted after the
drill; `--keep` keeps it for a follow-up look (it is still never committed).
Credentials for the source reach tq only through `tq dotenv run --`; never read
`.env` files and never print a connection string.

## 2. Run it

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 1860"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 1860"
$_to tq pack db drill <name>; echo "drill_exit=$?"
```

- The restore runs with `drill.timeout` (30 min by default); the outer timeout
  is that plus 60 s. If your shell tool cannot wait that long, run it in the
  background, or ask the human to run the same line in their terminal.
- It refuses a non-local target before it connects: fix `test_db`, never point
  it at a shared server.
- tq restores into `tq_drill_<name>_<ts>`, checks the migration version table
  against the newest migration file, counts the rows of each `verify` table,
  counts tables and indexes, writes the evidence line and drops the scratch
  database.

## 3. The evidence

One JSON line is appended to `docs/ops/restore-drills.jsonl` (create the file
empty from `templates/packs/db/restore-drills.jsonl` in the plugin when the
project has none). It holds `name`, `engine`, `backup_taken_at`, `restored_at`,
`duration_s`, `ok`, `tables` (name and count), `migration_version`,
`tool_versions` and `host_kind: local`. Only numbers and names: never row
content, never a host or a connection string. Never edit or delete earlier
lines, and never write a line by hand: only tq writes them.

Stage that file by name and commit it with the work, on the feature branch
(never on main or master, never `git add -A`).

## 4. When it fails

| Symptom | What you do |
|---|---|
| `ok: false`, a table count is 0 | the dump has no data (a schema-only `supabase db dump`?); fix the source and run again |
| migration version gap | the backup predates migrations in this diff; record it in the PR and say whether the restore path still works |
| timeout | report the duration; the human may raise `drill.timeout` |
| `db/restore-drill` stale | run step 2, then `tq pack check --pack db` again |
| the source needs a network fetch that fails | report it; never switch the drill to a production target |

The drill's age is part of the `ship` PR body (the db on-ramp notes) and of the
`db-reviewer`'s row `D12`.
