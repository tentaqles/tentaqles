---
name: ship
description: Take a working tree all the way to a review-ready pull request — identity check, feature branch, risk triage (tq decide triage), review depth picked by risk, local tests/lint, conventional commit of explicit paths, push, gh pr create with risk flags and verification evidence, then hand off to babysit-pr. Use when the user says "ship it", "ship this", "open a PR", "commit and push", "make a PR for this", "send this for review", "/ship", or wants finished work turned into a pull request. Does not merge.
---

# Ship

Turn the current changes into a pull request that a reviewer can trust. The
skill ends when the PR is open and handed to `babysit-pr`. It never merges.

**Proof rule.** CI output on the PR is the only accepted proof that the change
works. Local test runs are evidence you attach, not proof. Never write
"tests pass", "green" or "CI passes" in the PR body, a commit message or your
reply unless you are quoting CI output you actually read. Until then the
status is "CI pending".

Run the steps in order. Stop and tell the user when a step fails; do not skip
ahead.

## 1. Identity (light)

The tq guard already blocks a commit or push under the wrong identity, so this
is a quick sanity check, not a ceremony:

```bash
git config user.email
git remote get-url origin
command -v tq >/dev/null 2>&1 && tq doctor 2>&1 | tail -n 15
```

Compare the email with the workspace manifest (`.tentaqles.yaml` → `git.email`)
or the project's `CLAUDE.md`. If they disagree, stop and point the user to
`tq doctor` / `/tentaqles:switch-client`. Never change `user.email` yourself.

## 2. Branch

```bash
default=$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null || git symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null | sed 's@^origin/@@')
current=$(git branch --show-current)
echo "default=$default current=$current"
```

If `current` is the default branch (or empty, i.e. detached HEAD), create a
branch named after the change: `git switch -c <type>/<short-slug>` (for
example `fix/webhook-retry`, `feat/export-csv`). Uncommitted changes come
along. Never commit directly to the default branch.

## 3. Stage explicit paths

```bash
git status --short
```

Read the list and stage **only** the files that belong to this change, by
path: `git add path/one path/two`. Never `git add -A`, `git add .` or
`git commit -a` blindly. Leave out:

- `.env*` files, keys, credentials, local config (`settings.local.json`, `*.pem`)
- build output, caches, editor files, large binaries
- unrelated edits — mention them to the user instead

Then confirm what is staged: `git diff --staged --stat`.

## 4. Risk triage

```bash
tq decide triage --staged; echo "triage_exit=$?"
```

| Result | Meaning | Review |
|---|---|---|
| exit 0, `low` | no risk flag raised | light self-review |
| exit 3, `high (flags…)` | a flag fired, the diff was truncated, or Jev was unavailable | full review |
| any other exit, or `tq` missing | triage not available | treat as **high** |

**Light self-review (low):** read `git diff --staged` end to end yourself.
Look for leftover debug output, commented-out code, hardcoded values, missing
error handling and accidental files. Fix and re-stage.

**Full review (high):** spawn a reviewer subagent (the `Agent` tool, or the
`code-review` skill when available) with:

- the staged diff (`git diff --staged`), or the paths to read
- the triage flags (auth, money, schema, migrations, RLS, secrets, public API,
  destructive commands, …) as the areas to focus on
- the instruction to report concrete problems with file:line, not style nits

Fix every real finding, re-stage, and re-run triage. Record the flags and what
the review found; they go into the PR body.

## 5. Verify locally

Detect the project's own commands. Read them; don't guess:

| File | Commands to look for |
|---|---|
| `package.json` | `scripts.test`, `scripts.lint`, `scripts.typecheck` / `check`; runner from the lockfile (`pnpm-lock.yaml` → pnpm, `yarn.lock` → yarn, `bun.lockb` → bun, else npm) |
| `pyproject.toml` / `setup.cfg` / `tox.ini` | `pytest` (with `[tool.pytest]`), `ruff check` (with `[tool.ruff]`), `mypy` if configured; use `uv run` when `uv.lock` exists |
| `go.mod` | `go test ./...`, `go vet ./...` |
| `Cargo.toml` | `cargo test`, `cargo clippy` |
| `Makefile` / `justfile` / `Taskfile.yml` | `test`, `lint`, `check` targets — prefer these when present |
| `CLAUDE.md` / `CONTRIBUTING.md` | any documented pre-PR command overrides the table |

Run the narrowest set that covers the change, then the full suite if it is
fast. If a command needs secrets from a `.env`, run it through tq — reading
`.env` files directly is denied:

```bash
tq dotenv run -- npm test
```

Long suites: run them in the background (`run_in_background`) or with the
`Monitor` tool instead of `sleep` loops.

If anything fails, fix it and go back to step 3. Keep the exact commands and
the last lines of their output (pass/fail counts) for the PR body. If there is
no test suite, say so plainly in the PR body; do not invent one.

## 6. Commit

Conventional commit: `type(scope): summary` in the imperative, at most ~72
characters, `type` one of `feat fix refactor perf test docs build ci chore`.
The body says **why**, not a file list. Follow any attribution or trailer rule
from the project or the conversation.

Write the message to a file outside the repo (your scratchpad directory, or
the system temp dir) with your file-writing tool, then commit with `-F`, which
avoids quoting problems on every shell:

```bash
git commit -F "<path-to-message-file>"
```

If the commit is rejected (a pre-commit hook or the tq secret scan), fix the
cause and create the commit again. Never use `--no-verify`.

## 7. Push and open the PR

```bash
git push -u origin HEAD
```

Never `--force` to the default branch (the guard denies it). On a feature
branch prefer a new commit over rewriting history.

First check whether the branch already has a PR:

```bash
gh pr view --json url,state -q '.url + " " + .state' 2>/dev/null || echo "no PR yet"
```

If an open PR exists, the push above already updated it; refresh its body with
`gh pr edit --body-file <path-to-body-file>` instead of creating a second one.
Otherwise write the PR body to a file the same way, then create it. Each Bash
call starts a fresh shell, so `$default` from step 2 is gone; type the branch
name it printed:

```bash
gh pr create --base <default-branch> --title "<same as the commit summary>" --body-file "<path-to-body-file>"
```

The body follows this shape:

```markdown
## Summary
<what changed and why, 2-5 bullets>

## Risk
Triage: <low | high (flags…) | unavailable → treated as high>
Review: <light self-review | full review by subagent: N findings, all addressed>

## Verification
Local (evidence, not proof):
- `<command>` → <last line: e.g. "42 passed in 3.1s">
CI: pending — this PR is not verified until checks are green.

## Notes
<follow-ups, things deliberately left out, rollout notes>
```

## 8. Hand off

Report the PR URL and the triage result, then continue with the
**`babysit-pr`** skill on that PR: it watches CI, fixes failures and answers
review comments. Only report "CI green" once `babysit-pr` has seen it.

## Never

- merge, and never `gh pr merge --admin`
- force-push the default branch, or disable/skip checks to get green
- claim green, passing or verified without CI output
- print, commit or rotate secrets; use `tq dotenv run -- <cmd>` when a
  command needs them
