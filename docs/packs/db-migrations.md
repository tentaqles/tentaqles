# DB: migration checks and the migrate gate

The db pack (release 4, spec D §10, §11, §14) lints Postgres and SQL Server
migrations and gates migrations against declared environments. RLS is on
[RLS](db-rls.md); restore drills on [Restore drill](db-restore-drill.md).

**Relaxed mode waives this gate.** In a workspace where the human granted `relaxed` (`tq allow --mode <ws> relaxed`), `tq/db-migrate-env` follows the guard mode: it is skipped, its denies included, so an agent's migration runs with no ask and no approval, even where the policy says `approve` or `deny`. A custom table follows its `db-destructive` value. Only `rule` (or `deny`) keeps the gate: `ask` turns a call the gate denies into a prompt the human can click through, with no approval checked. One exception: `tq/db-migrate-env` supersedes the core `tq/remote-migration`, so a custom table that skips `db-destructive` but keeps `prod-changes` at `rule` keeps this gate (the `prod-changes` value lifts the verdict back). Keep the default mode, `strict`, or a custom table with `db-destructive: rule` wherever the gate must hold. See [PACKS.md](../PACKS.md#known-behaviours).

## Config

```yaml
packs:
  db:
    enabled: true
    tools: [squawk, sqlfluff]                  # declared => required checks
    migrations:
      - {path: supabase/migrations, engine: postgres, migrator: supabase}
      - {path: db/mssql, engine: sqlserver, migrator: flyway}
    small_tables: [public.feature_flags]       # lock rules downgrade to warn
    environments:
      dev:  {match: {dotenv: [.env], hosts: [localhost]}, migrate: auto}
      prod: {match: {dotenv: [.env.prod], project_refs: [aaaaaaaaaaaaaaaaaaaa]}, migrate: approve}
```

- **Migrators:** `supabase`, `prisma`, `drizzle`, `golang-migrate`, `dbmate`,
  `flyway`, `sqitch`, `alembic`, `efcore`, `dacpac` and `raw`.
- **The repo layer** may add `migrations` paths and turn `sqlfluff_fail` on.
  For `environments` the rules of the cloud pack apply (a new environment with
  its own `match`, never a looser one); `small_tables`, `edition` and `drill`
  are trusted-layer only.
- The plugin's `templates/packs/db/packs-db.yaml` is a full example.

## Checks

| Check | Kind | What it does |
|---|---|---|
| `db/migration-lint` | built-in | the `PG0xx` (Postgres) and `MS0xx` (SQL Server) rules on the changed migrations: online indexes, rewrites and long locks, two-step constraints, `lock_timeout`, destructive changes without a contract step. A statement the lexer cannot judge for sure is `SQL001`, which no suppression clears |
| `db/migration-rollback` | built-in | a down migration, or a `### Rollback` plan for a forward-only migrator |
| `db/squawk` | external | squawk over the changed Postgres migrations |
| `db/sqlfluff` | external | sqlfluff over the changed migrations |

The rules and their fixes are in the `db-migration` skill
(`references/ddl-safety.md`, `references/review-checklist.md`).

- **Suppression:** `-- tq:allow <rule>: <reason>` on the statement.
- **Contract steps:** a drop or rename carries `-- tq:contract <expand ref>`
  and ships after the code stops using the old shape.
- An applied migration is never edited (`tq/migration-edit`): write a new one.

## Migrating an environment

The guard `tq/db-migrate-env` resolves every migration command (supabase,
prisma, alembic, flyway, liquibase, dbmate, goose, golang-migrate, knex,
sqitch, drizzle-kit, atlas, rails, artisan, Django, EF Core) to an environment
from its connection flags, a `tq dotenv run --file` wrapper, variables and
the Supabase project link, and applies that environment's `migrate` policy:
`auto`, `ask`, `approve` or `deny`. Without declared environments the core
`tq/remote-migration` rule asks as before.

For `migrate: approve`:

1. Ask: `tq pack db approve --env prod`. Agents may ask. The evidence binds the
   content of every migration file and the newest restore drill line. The
   request is refused when a declared drill is stale (a human may pass
   `--no-drill`) or a db check fails (a human may pass `--allow-failing`).
2. The human answers **in their own terminal** with `tq hub answer <card>
   <decision>`, typing back the binding; the board does not answer pack
   approvals.
3. Run the migration once, unchanged, in a command of its own
   (`supabase db push`). Changing a migration file or recording a new drill
   voids the approval. A target tq cannot read (a computed URL, a folder it
   cannot follow) is floored to the default environment and gets no approval.

**Unknown global flags.** A Supabase global flag tq does not know, before the
subcommand (`supabase --x v db push`), counts as a migration to the strictest
environment. Put it after the subcommand, or report it (see
[PACKS.md](../PACKS.md#known-behaviours)).

## Review

The `db_migration` flag (a migrations path changed), and the core flags
`schema`, `migration`, `rls`, `data_loss` and `destructive`, bring in
`db-reviewer`.
