# db: migrations

## kickoff

Only when the stack ADR names Postgres, Supabase, SQL Server or Azure SQL
(this section is read whether or not the db pack is enabled; never assume it
is):
- At human stop 1, propose the `packs.db` block from the plugin's
  `templates/packs/db/packs-db.yaml`: the `migrations` paths with their `engine`
  and `migrator`, `environments` with `prod: {migrate: approve}`, a local
  `test_db`, and in `tools` only the tools that `command -v squawk sqlfluff
  pg_prove supabase` finds on PATH. It is a proposal for the human, who edits
  the manifest and runs the trust step; you never edit `.tentaqles.yaml`.
  Headless: write it to `docs/privacy-manifest-proposal.yaml`, never the
  manifest.
- The first migration of the first slice sets `lock_timeout` (Postgres) or
  `LOCK_TIMEOUT` (SQL Server) and follows the `db-migration` skill.

## feature

- The work raises `db_migration` when it touches a migrations path (and the
  core flags `schema`, `migration` or `data_loss` from triage). Before any diff
  exists, pass them to `tq pack crew --flags db_migration,migration` and take
  the crew it prints (`db-reviewer`).
- Spec slice: a `## Migration plan` with the expand and contract steps, the
  deploy order, and `### Rollback` for forward-only migrators.
- Acceptance checks to add: "`db/migration-lint` reports no error on the new
  migrations", "`db/migration-rollback` is ok", and for a drop or rename "the
  contract step carries `-- tq:contract <expand ref>` and ships after the code
  stops using it".

## fix

- The regression test for a migration bug is the lint itself when a rule
  covers it (a fixture migration that `tq pack check --pack db` flags), or a
  pgTAP or integration test against the local `test_db`.
- Never fix a finding by editing an applied migration (`tq/migration-edit`):
  write a new one. A `-- tq:allow <rule>: <reason>` suppression needs a reason
  that holds.

## dod

- List one row per `pack:db/*` item: `db/migration-lint`,
  `db/migration-rollback`, and each declared external check (`db/squawk`,
  `db/sqlfluff`). `MISSING` for a required fail, with the first findings.
