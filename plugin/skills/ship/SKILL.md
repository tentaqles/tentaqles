---
name: ship
description: Take a working tree all the way to a review-ready pull request — identity check, feature branch, explicit staging, a self-assessed risk line with the reviewer agents on a high-risk diff, local tests and lint, conventional commit of explicit paths, push, gh pr create with Summary, Risk, Verification and Notes, then hand off to babysit-pr. Use when the user says "ship it", "ship this", "open a PR", "commit and push", "make a PR for this", "send this for review", "/ship", or wants finished work turned into a pull request. Does not merge.
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

tq already blocks a commit or push under the wrong identity, so this is a
quick sanity check, not a ceremony:

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

## 4. Risk (self-assessed)

Read `git diff --staged` end to end. Rate the change `low` or `high` yourself
and write the reason down in one line; it becomes the Risk line of the PR
body. It is **high** when the diff touches auth or permissions, money, a
schema or migration, RLS, secrets handling, a public API, shared state,
destructive commands, CI workflows or dependency manifests, prompts or model
calls, or a glob the project's `CLAUDE.md` lists as an architecture path.
Otherwise it is low.

**Low:** a light self-review. Look for leftover debug output, commented-out
code, hardcoded values, missing error handling and accidental files. Fix and
re-stage.

**High:** write the staged diff to a file under your scratchpad directory
(`git diff --staged > <file>`) and run the reviewer agents on it, each with
the diff path, the spec slice when one exists, `CLAUDE.md`,
`docs/LEARNINGS.md` and `docs/threat-model.md`:

- `critic` always;
- `security-reviewer` for auth, secrets, a public API, shared state,
  destructive commands, workflows or dependencies;
- `privacy-reviewer` when personal data is read, written, logged or exported;
- `llm-security-reviewer` for prompts, model calls, tools, MCP servers, hooks
  or skills.

Fix every `F` row (a file, a line and a concrete fix), re-stage, and re-run
the reviewer until it returns `RESULT: PASS`. A `U` row is a question for the
user, never something to guess. Record the verdicts; they go into the PR
body.

## 5. Verify locally

Detect the project's own commands. Read them; don't guess:

| File | Commands to look for |
|---|---|
| `CLAUDE.md` | the **Checks** line of `## Project config` (test, lint, typecheck) overrides the rows below |
| `package.json` | `scripts.test`, `scripts.lint`, `scripts.typecheck` / `check`; runner from the lockfile (`pnpm-lock.yaml` → pnpm, `yarn.lock` → yarn, `bun.lockb` → bun, else npm) |
| `pyproject.toml` / `setup.cfg` / `tox.ini` | `pytest` (with `[tool.pytest]`), `ruff check` (with `[tool.ruff]`), `mypy` if configured; use `uv run` when `uv.lock` exists |
| `go.mod` | `go test ./...`, `go vet ./...` |
| `Cargo.toml` | `cargo test`, `cargo clippy` |
| `Makefile` / `justfile` / `Taskfile.yml` | `test`, `lint`, `check` targets — prefer these when present |
| `CONTRIBUTING.md` | any documented pre-PR command overrides the table |

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

**Definition of done.** When this branch came from `/tentaqles:feature`,
`/tentaqles:fix` or `/tentaqles:kickoff`, run `/tentaqles:dod` first and carry
its record into the PR body. An item that is not done is a line under Notes,
in the user's words; it is never dropped silently and never written as done.

**Size advisory** (not a block):

```bash
git diff --staged --shortstat
```

Over about 400 changed lines, the PR body needs a stated reason (a generated
file, a migration, a rename) under Notes; otherwise suggest splitting.

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

Never `--force` to the default branch. On a feature branch prefer a new
commit over rewriting history.

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
<low | high>: <why, in one line: the areas the diff touches>
Review: <light self-review | critic[, security-reviewer, privacy-reviewer, llm-security-reviewer]: N findings, all addressed>

## Verification
Local (evidence, not proof):
- `<command>` → <last line: e.g. "42 passed in 3.1s">
Definition of done: <PASS | not applicable | the MISSING items, each as a Notes line>
CI: pending — this PR is not verified until checks are green.

## Notes
<follow-ups, things deliberately left out, rollout notes>
Size: <N files, +A −D>; reason if over ~400 lines: <reason>
```

The PR body never contains personal data or secrets: write the shape of a
value (`<email>`, `<card>`, `<row id>`), never the value, and quote command
output only by its last line, never a tail that can name files or rows that
hold such data.

## 8. Hand off

Report the PR URL and the risk line, then continue with the
**`babysit-pr`** skill on that PR: it watches CI, fixes failures and answers
review comments. Only report "CI green" once `babysit-pr` has seen it.

## Never

- merge, and never `gh pr merge --admin`
- force-push the default branch, or disable/skip checks to get green
- claim green, passing or verified without CI output
- print, commit or rotate secrets; use `tq dotenv run -- <cmd>` when a
  command needs them
