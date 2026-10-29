# db: backup and restore drill

## kickoff

Only when the stack ADR names Postgres, Supabase, SQL Server or Azure SQL and
the project keeps data that must survive (this section is read whether or not
the db pack is enabled; never assume it is):
- Propose a `packs.db.drill` entry (`name`, `engine`, `source.kind` and its
  variable name, `verify` tables, `freshness`) in the same `packs.db`
  proposal, for the human; never edit the manifest yourself.
- Plan the first `restore-drill` run as a checkpoint item once the first
  migrations exist, and create `docs/ops/restore-drills.jsonl` empty from
  `templates/packs/db/restore-drills.jsonl`.

## feature

- A change with a destructive or risky migration (a drop, a contract step, a
  backfill) adds the acceptance check "the newest restore drill is within its
  freshness" (`db/restore-drill`). When it is stale, run the `restore-drill`
  skill before the DoD.

## dod

- List `pack:db/restore-drill` when a drill is declared and a migration
  changed, with the drill's age against its freshness.

## ship

- The `## Packs` section of the PR body gains the drill age per declared drill
  (name, days since the newest `ok` line, the freshness), next to the
  `pack:db/*` check ids and results. Never row counts of personal tables beyond
  what the evidence line holds, and never a host or connection string.
