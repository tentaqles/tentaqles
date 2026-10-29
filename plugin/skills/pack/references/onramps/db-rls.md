# db: row-level security

## kickoff

Only when the stack ADR names Postgres or Supabase with tables exposed to
clients (this section is read whether or not the db pack is enabled; never
assume it is):
- Propose `packs.db.rls` (`exposed_schemas`, from `supabase/config.toml`
  `[api] schemas` on Supabase, else `[public]`; `tests`, the pgTAP directory)
  in the same `packs.db` proposal as the migrations note, for the human; never
  edit the manifest yourself.
- Plan the `tests_tq` helper (`templates/packs/db/tests_tq_helpers.sql`, the
  `rls-tests` skill) as a checkpoint item of the first slice that creates a
  table.

## feature

- The work raises `rls_change` when it touches a Postgres migration, a pgTAP
  test or `supabase/config.toml` (and the core flag `rls` from triage). Take
  the crew from `tq pack crew --flags rls_change,rls` (`db-reviewer` and
  `security-reviewer`).
- Acceptance check to add: "every new exposed table has RLS and a pgTAP file",
  checked by `db/rls` and `db/rls-tests`, plus one line per policy saying who
  may read and write which rows.

## fix

- A policy bug's regression test is a pgTAP test (`rls_<table>.test.sql`,
  the `rls-tests` skill) that fails before the fix: the wrong user sees or
  writes the row. Synthetic users only.

## dod

- List `pack:db/rls`, `pack:db/rls-tests` and, when `pg_prove` or `supabase`
  is declared, the pgTAP item: `pack:db/pgtap-supabase` when `supabase` is
  declared (or a Postgres migrations entry uses the `supabase` migrator and
  `pg_prove` is not declared), otherwise `pack:db/pgtap`. Either failing with
  "test_db is not local" means the config points at a shared database: report
  it to the human, never point the tests at another server.
