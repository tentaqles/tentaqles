# cloud: plan, approve and apply

## build

- A checkpoint that applies infrastructure records its plan first with
  `tq pack cloud plan --env <env> -- <plan command>` (the `iac-policy` skill,
  step 3) and works from the redacted summary only. Never read a plan or state
  file and never print one (`tq/plan-read`, `tq/plan-print`).
- In an `approve` environment the checkpoint stops at `tq pack cloud approve
  <plan-id>` and waits for the human's answer on the board. You never answer it.

## fix

- `--hotfix` skips the critic, never the apply approval: a hotfix to prod is
  still `approve`. Record the plan, ask with `tq pack cloud approve`, wait for
  the human, then apply the saved plan once.
- When the fix is to the gate's input (a wrong environment match, a wrong
  identity), say so and propose the manifest change to the human; never edit
  `.tentaqles.yaml` or `tq-packs.yaml` to get an apply through.

## dod

- When a plan was recorded for this work, add its plan summary id (from
  `tq pack cloud plan` or `tq pack approvals --pack cloud`) to the
  `pack:cloud/*` rows, with the environment and the approval state (pending,
  approved, used, expired). Never the plan's values.

## ship

- The PR body gains a `## Packs` section (shared with the other pack notes):
  each `pack:cloud/*` check id and result, and per recorded plan the summary
  (counts per action, stateful creates, deletes and replaces, the environment,
  the approval state). It never includes a plan, a plan path, state, an output
  value or an identity's secret, and never claims an apply that the approval
  ledger does not show as used.
