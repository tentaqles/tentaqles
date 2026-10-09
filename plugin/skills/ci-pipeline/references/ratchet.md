# Ratchet metrics and collectors

## Which metrics

Start with three or four. Each must be deterministic (same code, same number)
and cheap to compute.

| Metric | Direction | Node / TS | Python | Go |
|---|---|---|---|---|
| `lint_errors` | lower | `eslint . -f json -o eslint.json` -> `--eslint` | `ruff check --output-format json -o ruff.json` -> `--ruff` | `staticcheck -f json ./...` -> `--count-lines lint_errors=FILE`, or golangci-lint JSON -> `--golangci` |
| `lint_warnings` | lower | from the same ESLint report | - | - |
| `coverage_pct` (lines) | higher | istanbul `json-summary` reporter -> `--coverage-summary coverage/coverage-summary.json` | `pytest --cov --cov-report=xml` -> `--cobertura coverage.xml` | `go tool cover -func=cover.out > cover.txt` -> `--go-cover cover.txt` |
| `duplication_pct` | lower | `jscpd --reporters json --output jscpd` -> `--jscpd jscpd/jscpd-report.json` | same (`--format python`) | same (`--format go`) |
| `complex_functions` (CCN > 10/15) | lower | ESLint `complexity` rule counts as lint, or a lizard report via `--count-lines` | `radon cc -j .` -> `--radon-cc` (threshold `--complexity-threshold`, default 10) | `gocyclo -over 15 .` -> `--count-lines complex_functions=FILE` |
| anything else | your call | `--metric NAME=VALUE:lower` or `--count-lines NAME=FILE:higher` | | |

Other good candidates: files over a size limit, largest file lines, circular
dependencies (dependency-cruiser, import-linter), `npm audit` high count,
type-check errors in a partially typed codebase (`tsc` / `mypy` error count),
bundle size in kB, and for RAG/ML projects an eval score (retrieval
recall@k, golden-set accuracy) with direction `higher`.

## Rules of thumb

- **Tolerance:** coverage can wobble by a few hundredths with parallel or
  randomized tests. Give `coverage_pct` a `tolerance` of 0.1 if it flaps;
  keep counts (lint, complexity) at 0 tolerance.
- **Missing = failure.** If a collector stops producing a metric (tool
  renamed, report path changed), `compare` fails instead of passing.
- **Crash vs findings.** Linters exit non-zero when they find problems. In
  the templates `|| [ $? -eq 1 ]` accepts "found issues" but still fails the
  step on exit 2+ (crash, bad config). Never use a bare `|| true`: an empty
  report would read as "0 errors" and look like an improvement.
- **Baseline updates happen in PRs**, by the author running `--update`
  locally or in a refactor PR. CI never commits to the repo, which keeps
  the workflow at `contents: read`.
- **Scope the tools** to source directories (exclude `dist/`, `vendor/`,
  generated code, migrations) so numbers track code people write.
- **One baseline per repo.** In a monorepo, prefix metric names per package
  (`web.lint_errors`, `api.coverage_pct`) using `--metric`, or run one job
  and baseline per package.

## Local loop

```bash
# Node example; same commands as the workflow
npx eslint . -f json -o eslint.json; npx vitest run --coverage --coverage.reporter=json-summary
python .github/quality/ratchet.py collect --out current.json --eslint eslint.json \
  --coverage-summary coverage/coverage-summary.json
python .github/quality/ratchet.py compare --baseline .github/quality/baseline.json --current current.json
```

Add `current.json`, `eslint.json`, `ruff.json`, `coverage/`, `jscpd/`,
`radon.json`, `cover.*`, `staticcheck.json`, `gocyclo.txt` and
`ratchet-report.json` to `.gitignore`.

## Agent loop

With the gate in place, an agent can be told "iterate until CI is green"
safely: the exit condition is objective. On failure it downloads the
`quality-reports` artifact (`gh run download <run-id> -n quality-reports`),
reads `ratchet-report.json`, and fixes the regressed metric rather than
editing the baseline.
