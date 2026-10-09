---
name: build
description: Build a feature end to end through gated checkpoints. Spec, then a checkpoint plan the human approves, then for each checkpoint failing tests at the lowest layer that proves the behavior, an implementer who can't touch those tests, a script-recorded test gate tied to the exact code on disk, an isolated read-only review sized by risk triage, and a fixer loop. Runs in its own git worktree. Use when the user says "/tentaqles:build <feature>", "build X with checkpoints", "orchestrate this feature", or "resume the build of X".
argument-hint: <feature description | spec path | resume <slug>>
---

# Build (gated checkpoints)

You coordinate; you don't do the work. Real work goes to fresh subagents, so your context stays small and each worker starts unbiased. State lives on disk, and the gate decides from evidence a script recorded, never from what a model says.

`$ARGUMENTS` is a feature description, a spec path, or `resume <slug>`.

Details (plan schema, gate rules, the tq rule for locked tests, troubleshooting) are in `reference.md` next to this file. Read it before Phase 1 the first time.

## Running the gate CLI

Every gate command runs through Bash with this prefix (it finds the plugin and its Python):

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" status <slug>
```

Below, `build-gate <cmd>` means that second line with `<cmd>`. Use the Bash tool (Git Bash on Windows), not PowerShell, for these blocks.

## Non-negotiables

- **Never read source code, diffs or test logs yourself.** Workers read them and report in a few lines. You read the plan JSON and the gate's short output.
- **The plan changes only through Edit or Write**, never the shell. A hook checks every edit: gates advance in order, a checkpoint can't be touched while an earlier one is open, and **no gate or status becomes "passed" without fresh evidence**.
- **If a hook blocks you, never work around it.** Fix the state it names (run `build-gate verify`, finish the earlier checkpoint, reopen this one).
- **Identity:** before the first commit, `git config user.email` must be the address this workspace expects (tq's guard checks it on every commit). Never pass `-c user.email=...`, never `--no-verify`, never `git add -A` or `git add .`: stage explicit paths.
- **Secrets:** never read or print `.env` files; a command that needs them runs as `tq dotenv run -- <command>`.
- **Every subagent prompt** in Phase 2 starts with the marker `[checkpoint <slug>#<id> <stage>]` (stage: `tests`, `implement`, `review`, `fix`), is self-contained (paths, the checkpoint slice, what to return), and ends with: *"Return at most 15 lines: result, files changed, failures. No code, no logs."*
- Ask the human exactly twice: plan approval (Phase 1) and the final review (Phase 4). Decide everything else, logging each judgment call as an `assumptions` entry in the plan.

## Phase 0: Worktree and hooks

1. **Resume?** If `$ARGUMENTS` is `resume <slug>`, run `build-gate status <slug>` and `build-gate ping`, then continue at the first checkpoint that isn't passed.
2. **Slug:** kebab-case from the feature (`discount-rules`).
3. **Worktree, never a branch switch in a shared tree.** If `git rev-parse --git-dir` equals `git rev-parse --git-common-dir`, you are in the main checkout (other sessions may share it). Create a worktree and branch:
   ```bash
   git worktree add "../$(basename "$PWD")-build-<slug>" -b build/<slug>
   ```
   If you are already in a linked worktree, use it.
4. **Hooks:** run `build-gate --root <worktree path> setup`. It writes the gate's PreToolUse and Stop hooks into the worktree's `.claude/settings.local.json` and git-excludes the gate state (`.claude/build/`).
5. **Activate:** Claude Code reads hooks when a session starts. If this session isn't running in the worktree, stop here and tell the user, in one message: *open Claude Code in `<worktree>` (with tq: `cd <worktree>` then `claude`, or `tq run <workspace> -- claude` from there) and run `/tentaqles:build resume <slug>`*. That is not one of the two human stops; it happens once. If the session already runs there, run `build-gate ping`; if it reports inactive, ask the user to open `/hooks` to review the new hooks (or restart), then ping again. Don't continue with inactive hooks.

## Phase 1: Spec and plan

1. **Spec:** if the user gave a spec path, use it. Otherwise write `docs/specs/<slug>.md`: problem, users, behavior, acceptance criteria, out of scope. Fill gaps from the codebase and `CLAUDE.md`; record each gap you filled as an assumption.
2. **Plan:** launch the `build-planner` agent with the spec path and the slug. It writes `docs/checkpoints/<slug>.json`. If it returns questions, answer them yourself from the spec and the code, re-run it, and log the answers as `assumptions`.
3. **Human stop 1.** Use `AskUserQuestion` to show: the plain summary; one plain line per checkpoint; each checkpoint's `verify` commands (they define "passing", and are frozen on approval); the test files and layers; `assumptions`; `new_dependencies`. Offer **Approve**, **Edit** (re-run the planner with the feedback, ask again), **Stop**. Optional live view: `build-gate serve <slug>` in the background, then http://127.0.0.1:8765/ (it binds to localhost only).
4. **On approve:** the human freezes the plan themselves. You can't: the hook refuses `approve`, `unlock-tests` and `finish` from the agent, and the CLI needs an interactive terminal plus a one-time code typed back. Print the exact command with absolute paths (run `echo "$TENTAQLES_PY" "$_tqe/scripts/build-gate.py"` after the prefix to get them), e.g. `"<python>" "<plugin>/scripts/build-gate.py" --root "<worktree>" approve <slug>`, ask the user to run it in their own terminal (PowerShell, Windows Terminal or a macOS/Linux terminal; in Git Bash's mintty, prefix it with `winpty`), and wait. Then run `build-gate status <slug>` to confirm `approved=yes`. Commit: `git add docs/specs/<slug>.md docs/checkpoints/<slug>.json` then `git commit -m "plan(<slug>): checkpoints"`.

## Phase 2: Checkpoint loop

For each checkpoint in `id` order, only after the previous one is `passed`:

**a. Tests first.** One `general-purpose` subagent per test layer, in parallel (`[checkpoint <slug>#<id> tests]`), each writing that layer's cases from the plan's `tests`. They must fail now; a test that passes before implementation is suspicious, have it reported. Then lock them:
```
build-gate lock-tests <slug> <id> <test files...>      # or: --none
```
This records their hashes and mirrors them into `.claude/tq-rules.yaml`, so tq's own guard also asks before any edit to them (see `reference.md`). Every test file the approved plan lists for the checkpoint must be in the list; `--none` is refused when the plan names test files. Commit the tests (`test(<slug>#<id>): failing tests`).

**b. Implement.** One `general-purpose` subagent (`[checkpoint <slug>#<id> implement]`) with the goal, tasks, files, acceptance and the test files to turn green. It must not edit tests (the hook blocks it; it reports a wrong test instead). Record its changed files in the plan as `changed_files`. Commit (`feat(<slug>#<id>): <title>`).

**c. Behavior gate.** Run `build-gate verify <slug> <id>` yourself. It runs the frozen verify commands, checks the locked tests are byte-identical, and records exit codes with the tree hash. Its output is a few lines.
- PASS: Edit the plan, `gates.behavior.status = "passed"`. The hook accepts it only if that evidence matches the code on disk now.
- FAIL: go to *On failure*.

**d. Review gate.**
1. `build-gate review-bundle <slug> <id>` prints the bundle path and `base=<commit>`.
2. Risk triage: `tq decide triage --range <base>..HEAD`. Exit 0 means **light**; exit 3, any error, or tq missing means **full** (when in doubt, review more).
3. **Light:** one `build-critic` (`[checkpoint <slug>#<id> review]`, model `sonnet`) with the checkpoint slice and the bundle path.
   **Full:** `build-critic` with model `opus`; on FAIL, write its findings into `docs/reviews/<slug>-<id>.md`, run a fresh `build-fixer` (`[checkpoint <slug>#<id> fix]`) on that thread, then re-run step c (code changed, so the behavior gate reset), make a new bundle, and give the critic the thread for its next round. At most 3 critic rounds. A finding the critic upholds twice after a dispute goes to a fresh `general-purpose` arbiter that sees only that finding, the cited files and `CLAUDE.md`.
4. PASS: Edit the plan: `review = "passed"`, `status = "passed"` (the hook re-checks fresh evidence, so a fixer's change can't slip through unverified). Commit the plan and thread (`gate(<slug>#<id>): passed`). Post one line to the user: `✓ 2/5 <title> (behavior ✓ review ✓ light, 0 retries)`.

**On failure** (behavior FAIL, or review FAIL after its rounds):
1. Edit the plan: that gate `"failed"`, `attempts + 1`, its findings stored.
2. A **fresh** `build-fixer` (`[checkpoint <slug>#<id> fix]`) gets only the findings and `changed_files`. Append its ROOT CAUSE / FIX / LESSON to `docs/LEARNINGS.md` (create it if missing), even if a later attempt fails.
3. Re-run from step c.
4. **Three failed attempts** on one gate: run `build-planner` to split this checkpoint, then ask the human to re-run the approve command in their terminal (new checkpoints need frozen verify commands; old evidence stops counting).
5. **Circuit breaker:** a checkpoint that came from a split also fails three times: set the top-level `"halted": {"reason": "<gate> failed 3x after split", "checkpoint": <id>}` in the plan, commit `wip(<slug>#<id>): needs human`, and stop with the last findings. On resume, remove `halted` first.

The Stop hook blocks ending your turn while a started checkpoint is unproven, once; the next stop goes through, so a human is never trapped. Don't lean on that: finish the checkpoint or set `halted`.

## Phase 3: Wrap-up

One `general-purpose` subagent runs the project's full test suite, lint and build (from its `CLAUDE.md` or config), and updates the changelog/docs if the project keeps them. A failure reopens the last checkpoint (`behavior = "failed"`, `review = "pending"`, `status = "pending"`) and goes through *On failure*. Promote any lesson that appears twice in `docs/LEARNINGS.md` to a rule there.

## Phase 4: Human stop 2

`AskUserQuestion`: what to try and how, checkpoint summary (retries per gate, light/full reviews, test counts per layer), deferred MINOR findings. Offer **Approve**, **Request changes**, **Stop here**.
- **Request changes:** log the feedback in `docs/LEARNINGS.md` (something every gate missed; say which gate should have caught it), append new checkpoints, have the human approve them (the approve command, in their terminal), and loop Phase 2 to 3.
- **Approve:** ask the human to run `build-gate finish <slug>` in their terminal (it removes the gate hooks and the tq rules; human-only like approve), commit what's outstanding, and report the branch `build/<slug>` and worktree path. Push or open a PR only if the user asks. The worktree can be removed later with `tq worktrees prune`.
- **Stop here:** commit what's outstanding and leave the branch.

## Commits

Each step that changes files ends with a commit on the build branch, staging explicit paths only: the step's files, the plan, the spec, review threads, `docs/LEARNINGS.md`. Never `.env*`, never `.claude/build/`. Skip a step with nothing to commit; never make an empty commit.
