---
name: rls-tests
description: "Write and run pgTAP tests that prove Postgres and Supabase row-level security: one rls_<table>.test.sql per exposed table from the db pack template, with two synthetic users, anon and the service role, plus the tests_tq helper schema, then run them against a local test database through the db/pgtap check (db/pgtap-supabase in Supabase mode). Use when a migration adds a table in an exposed schema, when db/rls-tests fails with untested tables, when the user says \"test RLS\", \"prove the policies\", \"pgTAP\", \"can user A see user B's rows\", or when /tentaqles:feature needs the acceptance check that every new exposed table has RLS and a pgTAP file."
argument-hint: "[<schema.table> ...] [--run]"
---

# RLS tests

Policies that nobody tests are policies that nobody knows work. The db pack's
`db/rls` check judges the policies from the migrations; this skill proves them
by running queries as real roles against a local database. The `db/rls-tests`
check fails when a table in an exposed schema with RLS has no test file that
names it. The pgTAP check runs the files and is required when `pg_prove` or
`supabase` is in `packs.db.tools`. It is `db/pgtap` (pg_prove), or
`db/pgtap-supabase` (`supabase test db`) when `supabase` is declared, or when a
Postgres migrations entry uses the `supabase` migrator and `pg_prove` is not
declared; the DoD item is `pack:db/pgtap` or `pack:db/pgtap-supabase`.

## 1. Find what needs a test

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 130"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 130"
$_to tq pack check --pack db --builtin-only --budget 100s; echo "check_exit=$?"
echo "rls_template=$_tqe/templates/packs/db/rls_table.test.sql"
echo "helpers=$_tqe/templates/packs/db/tests_tq_helpers.sql"
```

`db/rls-tests` lists the untested tables; `db/rls` lists policy findings
(`RLS001`…`RLS010`). Fix the `db/rls` errors in a new migration first (the
`db-migration` skill): a test of a broken policy only proves it is broken. The
tests directory is `packs.db.rls.tests` (for Supabase usually
`supabase/tests/database`).

## 2. Install the helpers once

Copy `templates/packs/db/tests_tq_helpers.sql` from the plugin to
`<packs.db.rls.tests>/000_tests_tq_helpers.sql`. It creates the `tests_tq`
schema:

| Helper | Does |
|---|---|
| `tests_tq.create_user(name)` | inserts a synthetic user into `auth.users` (on plain Postgres it first creates a stub `auth.users` and an `auth.uid()` that reads `request.jwt.claims`) |
| `tests_tq.uid(name)` | the stable id of that user |
| `tests_tq.as_user(name)` | `set local role authenticated` plus the JWT claims for that user |
| `tests_tq.as_anon()` / `tests_tq.as_service()` | the `anon` role, or `service_role`, which bypasses RLS |

It lives under the tests directory, never under the app's migrations, and it
commits so the later files can use it. Never edit an applied app migration to
add it.

## 3. Write one file per table

For each table, fill `templates/packs/db/rls_table.test.sql` and write it as
`<packs.db.rls.tests>/rls_<table>.test.sql`:
- `{{table}}` is the schema-qualified name (`public.notes`), so the coverage
  check's word match finds it; `{{owner_col}}` is the column the policies
  compare with `(select auth.uid())`; `{{col}}` is another writable column. Add
  the table's other `NOT NULL` columns to the inserts.
- Keep the six assertions: tester-a sees only own rows, cannot write as
  tester-b, cannot update tester-b's rows, anon sees nothing, the service role
  sees all, RLS is enabled. Add one assertion per extra policy (a tenant or team
  rule, a public read policy that is meant to exist) and raise `plan(n)` to the
  new count.
- Users are synthetic: `tester-a` and `tester-b`, never real data, never a row
  copied from a dump (`tq/pii-fixture-dump` watches the tests directory).
- A table that is service-only (`-- tq:rls-deny-all <reason>`) gets a file that
  proves `authenticated` and `anon` see nothing and cannot write.

## 4. Run them against a local database

The target is `packs.db.test_db`, and it must be local: `localhost`,
`127.0.0.1`, `::1`, a Unix socket or a compose service named in
`test_db.hosts`. Anything else makes the pgTAP check (`db/pgtap` or
`db/pgtap-supabase`) fail with `test_db is not local` before anything
connects. tq resolves `test_db.url_env` through `tq
dotenv`; never read `.env` files and never print the URL.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 630"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 630"
$_to tq pack check --pack db --budget 600s; echo "check_exit=$?"
```

- tq starts the database with `test_db.start` when it does not answer (up to
  180 s), then runs `supabase test db` (Supabase: the files under
  `supabase/tests`, local stack) or `pg_prove --ext .sql -r <tests dir>`. Each
  `not ok` line comes back as `file:test-number`.
- Run the tests only through tq. Never run `supabase test db --linked` or
  `--db-url` against a hosted project: that is a shared database, and these
  files write to it.
- A test that fails because the role has no table grant (`permission denied`)
  tests the grant, not the policy: grant what the app's roles need in a
  migration, as Supabase does for `anon` and `authenticated`.

## 5. Finish

Re-run step 1 until `db/rls-tests` is `ok`. Stage the test files you wrote by
name (never `git add -A` or `git add .`). Commit on the feature branch, never
on main or master. In a build, the `db-reviewer` judges coverage (row `D11`).
