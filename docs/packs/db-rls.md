# DB: row-level security

For Postgres and Supabase, the db pack judges row-level security from the
migrations and runs pgTAP tests that prove it (spec D §12, §13).

## Config

```yaml
packs:
  db:
    tools: [pg_prove]                       # or supabase; declared => the pgTAP check is required
    rls:
      exposed_schemas: [public]             # Supabase: read from supabase/config.toml [api] schemas when unset
      tests: supabase/tests/database        # the pgTAP directory
    test_db:                                # local or ephemeral only
      url_env: TEST_DATABASE_URL            # a variable NAME, resolved through tq dotenv
      start: supabase start                 # optional; run before the tests
      hosts: [db]                           # extra local names (a compose service)
```

A `tq-packs.yaml` may add `exposed_schemas` (union) and set `rls.tests` when the
manifest does not. `test_db` is trusted-layer only, because its `start` runs.

## Checks

| Check | Kind | What it does |
|---|---|---|
| `db/rls` | built-in | replays every Postgres migration in order and judges the changed ones (`RLS001`-`RLS011`) |
| `db/rls-tests` | built-in | every table in an exposed schema with RLS has a test file that names it |
| `db/pgtap` | external (`pg_prove`) | runs the tests against `test_db` |
| `db/pgtap-supabase` | external (`supabase`) | runs the same tests with `supabase test db` |

**Which pgTAP check.** The tests run as `db/pgtap-supabase` when `supabase` is
declared in `tools`, or when a Postgres migrations entry uses the `supabase`
migrator and `pg_prove` is not declared. Otherwise they run as `db/pgtap`. The
DoD item is `pack:db/pgtap` or `pack:db/pgtap-supabase` accordingly.

**Local only.** `test_db` must be `localhost`, a loopback address, a Unix
socket or a name in `test_db.hosts`. Anything else fails with `test_db is not
local` before anything connects. The password is never passed to a tool that
resolves inside the repository.

### Rules

| Rule | What |
|---|---|
| `RLS001` | a new table in an exposed schema without `ENABLE ROW LEVEL SECURITY` in the same migration (a table moved into an exposed schema, or created inside `CREATE SCHEMA`, counts) |
| `RLS002` | RLS enabled with no policy (mark a service-only table `-- tq:rls-deny-all <reason>`) |
| `RLS003` | `DISABLE` / `NO FORCE ROW LEVEL SECURITY` or `DROP POLICY` with no replacement in the same migration |
| `RLS004` | a policy for `anon` or `authenticated` with `USING (true)` or `WITH CHECK (true)` |
| `RLS005` | a policy that reads `user_metadata` / `raw_user_meta_data`, which users can edit |
| `RLS006` | `GRANT ALL` or a write grant to `anon`, a grant to `public` in an exposed schema, default privileges to `anon` |
| `RLS007` | a view in an exposed schema without `security_invoker = true`, or a materialized view or foreign table there (also when moved there with `SET SCHEMA`) |
| `RLS008` | a `SECURITY DEFINER` function in an exposed schema without `SET search_path`, or one `anon` can execute (`-- tq:rls-anon-exec <reason>`) |
| `RLS009` | `auth.uid()` called per row instead of `(select auth.uid())` |
| `RLS010` | `service_role` in a policy |
| `RLS011` | a `DO` block or routine body with dynamic RLS-relevant DDL the gate cannot judge |

Suppress a finding with `-- tq:allow RLS007: <reason>` before the statement; a
`tq:allow` without a reason is itself an error (`SUP001`).

## Writing the tests

The `rls-tests` skill writes one `rls_<table>.test.sql` per exposed table from
`templates/packs/db/rls_table.test.sql`, with the `tests_tq` helpers
(`templates/packs/db/tests_tq_helpers.sql`): two synthetic users, `anon` and the
service role. The `rls_change` flag (a Postgres migration, a pgTAP test or
`supabase/config.toml` changed) brings in `db-reviewer` and
`security-reviewer`.
