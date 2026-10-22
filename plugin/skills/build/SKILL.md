---
name: build
description: Build a feature end to end through gated checkpoints. Spec, then a checkpoint plan the human approves, then for each checkpoint failing tests at the lowest layer that proves the behavior, an implementer who can't touch those tests, a script-recorded test gate tied to the exact code on disk, an isolated read-only review sized by risk triage, and a fixer loop. Runs in its own git worktree. Use when the user says "/tentaqles:build <feature>", "build X with checkpoints", "orchestrate this feature", or "resume the build of X".
argument-hint: <feature description | spec path | resume <slug>>
---

# Build (gated checkpoints)

You coordinate; you don't do the work. Real work goes to fresh subagents, so your context stays small and each worker starts unbiased. State lives on disk, and the gate decides from evidence a script recorded, never from what a model says.

`$ARGUMENTS` is a feature description, a spec path, or `resume <slug>`.

When another skill hands you a run, it passes `workflow=<feature|fix|kickoff>` and (for a fix) `hotfix=<yes|no>`; pass both to the planner so the plan carries `"workflow"` and `"hotfix"`. A plan without them is a plain `build`.

Details (plan schema, gate rules, the tq rule for locked tests, troubleshooting) are in `reference.md` next to this file. Read it before Phase 1 the first time.

## Running the gate CLI

Every gate command runs through Bash with this prefix (it finds the plugin and its Python):

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" status <slug>
```

Below, `build-gate <cmd>` means that second line with `<cmd>`. Use the Bash tool (Git Bash on Windows), not PowerShell, for these blocks.

## Non-negotiables

- **Never read source code, diffs or test logs yourself.** Workers read them and report in a few lines. You read the plan JSON and the gate's short output.
- **The plan changes only through Edit or Write**, never the shell. A hook checks every edit: gates advance in order, a checkpoint can't be touched while an earlier one is open, and **no gate or status becomes "passed" without fresh evidence**.
- **If a hook blocks you, never work around it.** Fix the state it names (run `build-gate verify`, finish the earlier checkpoint, reopen this one).
- **Identity:** before the first commit, `git config user.email` must be the address this workspace expects (tq's guard checks it on every commit). Never pass `-c user.email=...`, never `--no-verify`, never a blanket stage-everything add: stage explicit paths.
- **Secrets:** never read or print `.env` files; a command that needs them runs as `tq dotenv run -- <command>`.
- **Every subagent prompt** in Phase 2 starts with the marker `[checkpoint <slug>#<id> <stage>]` (stage: `tests`, `implement`, `review`, `fix`), is self-contained (paths, the checkpoint slice, what to return), and ends with: *"Return at most 15 lines: result, files changed, failures. No code, no logs."*
- Ask the human exactly twice: plan approval (Phase 1) and the final review (Phase 4). Decide everything else, logging each judgment call as an `assumptions` entry in the plan.

## Headless mode

The hub (release 2) and any `claude -p` caller set `TENTAQLES_HEADLESS=1`, or pass `--headless` in `$ARGUMENTS`. Then `AskUserQuestion` is never called. At every human stop you write the question as a decision record with `build-gate ask` and end your turn; the Stop hook lets a turn end while a decision is pending:

```bash
build-gate ask <slug> --kind plan-approval --text "<the plain summary, one line per checkpoint, verify commands, assumptions>" --options approve,edit,stop
build-gate ask <slug> --kind result --text "<what to try, checkpoint summary, deferred MINORs>" --options approve,request-changes,stop
build-gate ask <slug> --kind judge --checkpoint <id> --reviewer <name> --text "<the U rows>" --options PASS,FAIL
build-gate ask <slug> --kind question --checkpoint <id> --text "<the question>" --options <a>,<b>
```

Every stop that needs the human to re-approve (after a split, after Request changes) is also `build-gate ask <slug> --kind plan-approval ...`: answering `approve` is what freezes the new checkpoints; a `question` does not.

On resume (`/tentaqles:build resume <slug>`), read `build-gate state <slug> --json` first. `decisions` lists the latest record per decision with its `status`, `choice`, `note` and `actor`; `pending_decisions` is the unanswered subset. An answered record means continue from the answer (`plan-approval` `approve` → the plan is already frozen; `edit` → re-run the planner with the note; `result` `approve` → the run is finished (answering it runs `build-gate finish`); `request-changes` → Phase 4's request-changes path with the note; `question` `override` → the DoD override is in force (answering it ran it with the note as the reason); `stop` → commit and end). A pending decision means the human has not answered: end the turn again without working.

In interactive mode you write no decision records: the human stops stay `AskUserQuestion` and the terminal commands (`approve`, `finish`, `judge`, `dod --override`), as before. A pending record suspends the Stop guard, so one that no terminal command answers would switch it off for the rest of the run. The plan's top-level `task` block, `{"hub", "workspace", "kind", "external"}`, is copied verbatim from `TQ_HUB_TASK` by the on-ramp when the hub launched the run (the gate refuses a `task` the environment did not set, and the hub finds its build by `task.hub`); never invent one.

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

   **Headless** (`TENTAQLES_HEADLESS=1` or `--headless`): there is no user to reopen Claude Code. Instead of the message, end the turn with this exact line as the last line of your final output, nothing after it: `TQ_HUB_RELOCATE <absolute worktree path> <slug>`. Take the path from `git -C "<worktree>" rev-parse --show-toplevel` (a native path on Windows, `C:/...`, not Git Bash's `/c/...`). The hub starts the next round in the worktree with the same on-ramp (or `/tentaqles:build resume <slug> --headless` once the plan exists). Write the line only in headless mode and only once per slug: in the worktree session step 3 finds the linked worktree and this step runs `build-gate ping` instead.

## Phase 1: Spec and plan

1. **Spec:** if the user gave a spec path, use it. Otherwise write `docs/specs/<slug>.md`: problem, users, behavior, acceptance criteria, out of scope. Fill gaps from the codebase and `CLAUDE.md`; record each gap you filled as an assumption.
2. **Plan:** launch the `build-planner` agent with the spec path and the slug. It writes `docs/checkpoints/<slug>.json`. If it returns questions, answer them yourself from the spec and the code, re-run it, and log the answers as `assumptions`.
3. **Human stop 1.** Use `AskUserQuestion` to show: the plain summary; one plain line per checkpoint; each checkpoint's `verify` commands (they define "passing", and are frozen on approval); the test files and layers; `assumptions`; `new_dependencies`. Offer **Approve**, **Edit** (re-run the planner with the feedback, ask again), **Stop**. (Headless: write the `plan-approval` record from the Headless mode section instead and end the turn.) Optional live view: `build-gate serve <slug>` in the background, then http://127.0.0.1:8765/ (it binds to localhost only).
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

**d. Review gate: a crew, every verdict recorded.**
1. `build-gate review-bundle <slug> <id>` prints the bundle path and `base=<commit>`.
2. Risk triage: `tq decide triage --range <base>..HEAD`. Exit 0 means **light**; exit 3, any error, or tq missing means **full** (when in doubt, review more). Note the flags it prints.
3. Pick the crew. Always `build-critic` (model `sonnet` for light, `opus` for full). Add `spec-critic` when the plan's `workflow` is `feature` or `kickoff`, or it is `fix` and `changed_files` include files outside the failing path named in the fix spec. Add `security-reviewer` on flags auth, secrets, api, state, destructive, or when the diff touches `.github/workflows/` or a dependency manifest; `privacy-reviewer` on flag `classified_data` or a diff touching `classified_paths`; `llm-security-reviewer` on flag `agentic`; `breaker` only when the checkpoint is marked `"runnable": true` in the plan and Project config has a `run` command. (The security and privacy reviewers ship with the security pack; if an agent is not installed, record nothing for it and tell the user which reviewer was skipped and why.) Run the crew members in parallel, each with the prompt marker `[checkpoint <slug>#<id> review]`, the checkpoint slice, the bundle path, the spec slice path and its checklist source.
4. Each reviewer returns the contract (`RESULT`, `BUILT-BY-ME`, `F` and `U` rows). If `BUILT-BY-ME: yes`, discard it and run a fresh instance. Record every verdict, you never skip one:
   ```
   build-gate record-review <slug> <id> --reviewer build-critic --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N
   ```
   The hook refuses `review = "passed"` until the current code has a recorded `build-critic` PASS and no FAIL or BLOCKED beside it.
5. **FAIL from any reviewer:** write all findings into `docs/reviews/<slug>-<id>.md` (one section per reviewer), run a fresh `build-fixer` (`[checkpoint <slug>#<id> fix]`) on the thread, re-run step c (code changed, so the behavior gate reset), make a new bundle, and re-run only the reviewers that failed plus `build-critic`, giving them the thread. At most 3 rounds per checkpoint. A finding upheld twice after a dispute goes to the `arbiter` agent with only that finding, the dispute, the cited files and `CLAUDE.md`; its ruling is final for this run.
6. **BLOCKED from any reviewer:** the `U` rows are a human question. Use `AskUserQuestion` to show them (headless: write the `judge` record from the Headless mode section and end the turn; this does not count as one of the two stops: nothing proceeds without it anyway). The human settles each in their own terminal: `build-gate judge <slug> <id> --reviewer <name> --result PASS|FAIL --note "<why>"`. A FAIL ruling goes through step 5.
7. All PASS: Edit the plan: `review = "passed"`, `status = "passed"`. Commit the plan and thread (`gate(<slug>#<id>): passed`). Post one line to the user: `✓ 2/5 <title> (behavior ✓ review ✓ light crew: critic, spec-critic · 0 retries)`.

**On failure** (behavior FAIL, or review FAIL after its rounds):
1. Edit the plan: that gate `"failed"`, `attempts + 1`, its findings stored.
2. A **fresh** `build-fixer` (`[checkpoint <slug>#<id> fix]`) gets only the findings and `changed_files`. Append its ROOT CAUSE / FIX / LESSON to `docs/LEARNINGS.md` (create it if missing), even if a later attempt fails.
3. Re-run from step c.
4. **Three failed attempts** on one gate: first record `build-gate ask <slug> --kind plan-approval --text "split #<id>: <why>" --options approve,edit,stop` (in both modes: the gate refuses a plan edit that removes or renumbers an approved checkpoint unless a plan approval is pending), then run `build-planner` to split this checkpoint, then ask the human to re-run the approve command in their terminal, which also answers the record (headless: end the turn on the record) (new checkpoints need frozen verify commands; old evidence stops counting; until then a removed checkpoint counts as not passed).
5. **Circuit breaker:** a checkpoint that came from a split also fails three times: set the top-level `"halted": {"reason": "<gate> failed 3x after split", "checkpoint": <id>}` in the plan, commit `wip(<slug>#<id>): needs human`, and stop with the last findings. On resume, remove `halted` first.

The Stop hook blocks ending your turn while a started checkpoint is unproven, once; the next stop goes through, so a human is never trapped. Don't lean on that: finish the checkpoint or set `halted`.

## Phase 3: Wrap-up

When the plan's `workflow` is `feature`, `fix` or `kickoff`, run the definition of done yourself: `build-gate dod <slug>`. It runs the project's test, lint and typecheck commands frozen at approval, checks the locked tests, every checkpoint and reviewer record, and the obligations the diff created (an ADR for `architecture_paths`, `docs/privacy.md` for `classified_paths`, a `## Migration plan` in the spec for migration files, the changelog). `MISSING` items are work: fix them (a fixer subagent for code, you for docs), then re-run. The Stop hook blocks once, like the checkpoint rule, when a finished plan has no fresh DoD. Only a human can waive an item, and only by running the override themselves: you never run `build-gate dod <slug> --override`, and the hook refuses it with exit 2. If an item cannot be fixed, stop and ask the human to run `build-gate dod <slug> --override "<their reason>"` in their own terminal (headless: write `build-gate ask <slug> --kind question --text "override: <the MISSING items and why they cannot be fixed>" --options override,stop` and end the turn; the human answers with `build-gate answer <slug> <id> --choice override --note "<their reason>"`, which runs the override, or a hub actor token does). The terminal override also marks that pending question answered. Then confirm with `build-gate state <slug> --json`: `dod.ok` is true and `dod.override` is set. Re-running plain `build-gate dod <slug>` is safe, because the gate keeps a human override while the code is unchanged; any code change drops it and you ask again. The override is logged to `docs/LEARNINGS.md` and counted by `tq insights`. For a plain `build` plan, run one `general-purpose` subagent for the project's full test suite, lint and build as before, and have it update the changelog/docs if the project keeps them. A failure reopens the last checkpoint (`behavior = "failed"`, `review = "pending"`, `status = "pending"`) and goes through *On failure*. Promote any lesson that appears twice in `docs/LEARNINGS.md` to a rule there.

## Phase 4: Human stop 2

`AskUserQuestion` (headless: write the `result` record from the Headless mode section and end the turn): what to try and how, checkpoint summary (retries per gate, light/full reviews, test counts per layer), deferred MINOR findings. Offer **Approve**, **Request changes**, **Stop here**.
- **Request changes:** log the feedback as an H<n> line in `docs/LEARNINGS.md` (what was rejected and which gate should have caught it), append new checkpoints, have the human approve them (the approve command, in their terminal; headless: `build-gate ask <slug> --kind plan-approval ...`), and loop Phase 2 to 3.
- **Approve:** ask the human to run `build-gate finish <slug>` in their terminal (it removes the gate hooks and the tq rules; human-only like approve), commit what's outstanding, and report the branch `build/<slug>` and worktree path. Push or open a PR only if the user asks. The worktree can be removed later with `tq worktrees prune`.
- **Stop here:** commit what's outstanding and leave the branch.

## Commits

Each step that changes files ends with a commit on the build branch, staging explicit paths only: the step's files, the plan, the spec, review threads, `docs/LEARNINGS.md`. Never `.env*`, never `.claude/build/`. Skip a step with nothing to commit; never make an empty commit.
