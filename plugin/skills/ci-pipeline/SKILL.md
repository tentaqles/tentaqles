---
name: ci-pipeline
description: Set up or review a GitHub Actions CI pipeline with a ratchet quality gate (baseline.json of lint errors, coverage, duplication, complexity; CI fails when any metric gets worse) for Node/TypeScript, Python or Go. Use when adding CI to a repo, writing or reviewing .github/workflows files, adding a quality gate to a legacy codebase, pinning actions to SHAs, tightening workflow permissions, or when the user says "set up CI", "quality gate", "ratchet", "baseline", "coverage must not drop", "GitHub Actions" or "pipeline".
---

# CI Pipeline with a Ratchet Gate

A gate on absolute thresholds fails every legacy repo on day one, so nobody
turns it on. A **ratchet** freezes today's numbers in `baseline.json` and
fails a PR only if any metric gets worse, by even one lint error or 0.1
coverage point. Improvements lock in, so quality only moves one way. An
agent can then iterate unattended until CI is green, because the exit
condition is objective.

## 1. Detect the stack

Read `package.json` (scripts, test runner: Vitest or Jest, ESLint config),
`pyproject.toml` / `uv.lock` / `requirements*.txt`, or `go.mod`. Reuse the
project's own scripts: CI must run the same commands a developer runs
locally.

## 2. Install the gate

1. Copy the stack template from `templates/` (`node.yml`, `python.yml`,
   `go.yml`) to `.github/workflows/ci.yml` and adapt the commands.
2. Copy `scripts/ratchet.py` to `.github/quality/ratchet.py` (stdlib only,
   Python 3.8+, no install step).
3. Run the collect commands from the template locally, then create the
   baseline once:
   `python .github/quality/ratchet.py init --baseline .github/quality/baseline.json --current current.json`
   Before any code exists there is nothing to measure: write the metrics
   that cannot be measured yet as `"value": null` (unmeasured), never as a
   made-up `0`. A zero for `coverage_pct` would make the check a permanent
   no-op and a zero for `duplication_pct` would fail the first real run.
   Examples: [references/baseline.example.json](references/baseline.example.json)
   and [references/baseline.unmeasured.example.json](references/baseline.unmeasured.example.json).
4. Commit the workflow, script and baseline together. The template also has a
   `secrets` job (gitleaks); its job-level `pull-requests: read` is required
   because gitleaks-action lists a PR's commits.

Metric choice, collectors per stack and tolerances:
[references/ratchet.md](references/ratchet.md).

## 3. How the ratchet behaves

- `compare` exits 1 when a metric regressed **or a baseline metric is
  missing** (a broken collector must not pass silently), 2 on bad input.
- Improvements are reported; the PR author runs
  `ratchet.py compare --baseline ... --current current.json --update` and
  commits the tightened `baseline.json`. `--update` never loosens a value
  and does nothing when anything regressed. `--require-tight` makes CI fail
  until improvements are committed.
- A baseline value of `null` is "not measured yet": `compare` passes it and
  reports it as `unmeasured`. The first PR that has real code runs
  `compare ... --update` and commits the baseline; from then on the metric
  ratchets like any other. Until then it does not bind, so record the first
  measurement in the first PR that adds code. A collector that stops
  producing an unmeasured metric still fails (missing), the current file
  must be numeric, and `init --force` stays the only way to overwrite.
- Tests themselves must pass outright; only measured quantities ratchet.
- The JSON report and step summary are uploaded as an artifact so an agent
  can read why the gate failed.
- Never hand-edit the baseline to a worse value to get green. If a metric
  legitimately must move (deleting well-tested code lowers coverage), say so
  in the PR and have a human approve the baseline change.

## 4. Workflow security (non-negotiable)

- Pin every third-party action to a full commit SHA with the version in a
  comment; let Dependabot/Renovate bump them. tq denies
  `@main`/`@master` in workflow files (`tq/gha-branch-ref`) and asks on
  `@latest` (`tq/gha-unpinned`). `/tentaqles:supply-chain` applies the full checklist.
- Top-level `permissions: contents: read`; grant more per job only where needed.
- Never use `pull_request_target` together with a checkout of the PR's code
  (`ref: ${{ github.event.pull_request.head.sha }}`): that runs untrusted code
  with write tokens and secrets. Use `pull_request`.
- `actions/checkout` with `persist-credentials: false`.
- Secrets only via `${{ secrets.NAME }}` or OIDC to the cloud; never echo
  them, never put them in `run:` lines through `${{ }}` interpolation of
  untrusted input (titles, branch names): pass them through `env:`.
- `concurrency:` on every workflow; `timeout-minutes` on every job.
- Deploy jobs `needs:` the quality job, run migrations through the pipeline
  (see `db-migration`), and end with a smoke check. Deploy is not release:
  prefer feature flags for risky changes.
- Optional, when the workspace enables tq packs (cloud, db): a job step
  `tq pack check --base origin/<default> --json` with the tools declared in
  `packs.<pack>.tools` installed at pinned versions, so CI fails on the same
  `cloud/*` and `db/*` findings as the definition of done. The step is offline;
  the plan stays the project's step (`terraform plan`, `what-if`, `cdk synth`
  run with the project's own credentials, never inside `tq pack check`).

Locally, run the same commands with secrets from `.env` without printing
them: `tq dotenv run -- npm test`.

## 5. Review mode

When reviewing an existing workflow, report findings against section 4 plus:
deploy gated on green checks, lockfile installs (`npm ci`, `uv sync
--frozen`), branch protection requiring the check, artifacts uploaded on
failure. Run `tq decide triage --staged` on workflow changes; CI and secrets
changes come back `high`.

## 6. Scaffolding with /tentaqles:add-project

`add-project` stays stack-agnostic and does not write CI by itself. After it
scaffolds a project whose stack is known (Node/TS, Python, Go), it offers to
run this skill. A future `tq` or add-project step could do it
automatically: pick the template from the manifest's `stack`, copy
`ratchet.py`, run the collectors once and write the first baseline. Doing it
at project creation is the cheapest moment, since the baseline starts at
zero lint errors. Metrics that need code (coverage, duplication) start as
`null` and are recorded with `--update` in the first PR that has code.
