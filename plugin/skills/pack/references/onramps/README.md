# Pack on-ramp notes

The `build`, `feature`, `fix`, `kickoff`, `dod` and `ship` skills each have a
`## Packs` step (the `pack` skill, "On-ramp step"). For every pack that
`tq pack list --enabled --json` reports as enabled, that step reads each file here
whose name starts with the pack's name, and follows the section for the running
on-ramp.

## File convention

- One file per pack workstream, named `<pack>-<topic>.md`: lowercase, the pack name
  (`data`, `ml`, `cloud`, `db`), a hyphen, then a topic in `[a-z0-9-]`. For example
  `data-contracts.md`, `ml-promotion.md`, `cloud-iac.md`, `db-rls.md`.
- No other files here except this README. A workstream never edits another's file.
- Each file has one `##` section per on-ramp it changes, named exactly after the
  skill. Leave out the sections for on-ramps it does not change:

```markdown
# <pack>: <topic>

## build
## feature
## fix
## kickoff
## dod
## ship
```

- A section says what the on-ramp adds when this pack is enabled: spec slice fields,
  acceptance checks, regression-test layer, proposed `packs.<pack>` keys, the rows of
  the DoD item table for `pack:<pack>/*`, the PR body lines. Reviewers come from
  `tq pack crew --flags ...`; a section may name the flags the work raises, never a
  hard-coded crew.
- The notes are instructions to the agent, not to the human. They never tell the
  agent to run a human-only command (`tq pack baseline`, `tq pack ml promote apply`,
  `tq hub answer`, `build-gate approve`), to edit `tq-packs.yaml` or a baseline file
  to make a check pass, or to read `.env` files.
- Bash blocks follow the plugin conventions: they start with the identity-aware
  `_tqe` snippet, Python goes through `tq_run.sh` or `$TENTAQLES_PY`, and every `tq`
  call has an explicit timeout.
