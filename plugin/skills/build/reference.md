# /tentaqles:build reference

## Files

| Path | Owner | What |
|---|---|---|
| `docs/specs/<slug>.md` | orchestrator | the spec / PRD |
| `docs/checkpoints/<slug>.json` | orchestrator, via Edit/Write only | the plan (schema in `agents/build-planner.md`) |
| `docs/reviews/<slug>-<id>.md` | orchestrator + fixer | review thread (the critic is read-only; you write its findings) |
| `docs/LEARNINGS.md` | orchestrator | failures, root causes, lessons; repeated lessons become rules |
| `.claude/build/<slug>/approved.json` | gate | verify commands frozen at approval, locked test hashes |
| `.claude/build/<slug>/evidence.jsonl` | gate | one line per `verify` run: commands, exit codes, tree hash |
| `.claude/build/<slug>/session.json` | gate | owning session, current stage, stop counter, checkpoint base commits |
| `.claude/build/<slug>/review-<id>.md` | gate | the bundle a read-only reviewer reads |
| `.claude/settings.local.json` | gate (`setup`/`finish`) | the gate's hooks |
| `.claude/tq-rules.yaml` | gate (`lock-tests`/`finish`) | the locked-tests rule for tq's guard |

`.claude/build/`, `.claude/settings.local.json` and a gate-generated `tq-rules.yaml` are excluded through `.git/info/exclude`, so the project's `.gitignore` is not touched.

## Gate CLI

```
build-gate setup                         install hooks in .claude/settings.local.json (idempotent)
build-gate ping                          are the hooks active in this session?
build-gate approve <slug>                freeze verify commands             (hook asks the human)
build-gate lock-tests <slug> <id> <files...> [--none] [--action ask|deny]
build-gate unlock-tests <slug> <files...>                                   (hook asks the human)
build-gate verify <slug> <id> [--timeout 900]
build-gate status <slug>
build-gate review-bundle <slug> <id>     prints the bundle path and base=<commit>
build-gate serve <slug> [--port 8765]    progress page on 127.0.0.1 only
build-gate finish <slug>                 remove hooks and test locks        (hook asks the human)
```

Add `--root <path>` before the command to act on another checkout (e.g. a fresh worktree).

## Gate rules (what the hook enforces)

PreToolUse on `Agent|Task|Bash|PowerShell|Edit|Write|MultiEdit|NotebookEdit`, Stop on every turn end. Both have a 20 s timeout, read only local files and git, and never touch the network. An internal error lets the call through (exit 0) with a message: the hook must never stall a session.

**Agent markers** `[checkpoint <slug>#<id> <stage>]`:
- the plan must exist and be approved; the checkpoint must have frozen verify commands;
- every earlier checkpoint must be `passed`; this one must not be;
- `implement`, `review` and `fix` need the checkpoint's tests locked (`lock-tests`, or `--none`);
- `review` needs `behavior = passed` **and** fresh evidence.

**Plan edits** (Edit/Write/MultiEdit of `docs/checkpoints/<slug>.json`):
- `review` can't pass before `behavior`; `status: passed` needs every gate passed or n/a;
- gates or status can't advance while an earlier checkpoint is open;
- moving anything back toward `pending`/`failed` is always allowed (so a bad state can always be undone);
- a gate or status becoming `passed` needs the latest evidence for that checkpoint to be all exit 0 **for the current tree hash**.

**Evidence and the reset.** `verify` runs the frozen commands (bash on every OS) and records each exit code plus the *tree hash*: the git tree id of the working tree (tracked and untracked files, minus ignored files and the build's own bookkeeping: `docs/checkpoints`, `docs/reviews`, `docs/test-plans`, `.claude/build`). It is computed in a throwaway index, so committing the same content doesn't change it, but any code change does. That is how a gate resets: after a fixer touches code, the old evidence no longer matches, and nothing can be marked passed until `verify` runs again.

**Shell and file guards:**
- `.claude/build/` is never written by a tool or a shell command except the gate's own CLI;
- `docs/checkpoints/*.json` isn't written from the shell (Bash or PowerShell), only through Edit/Write so the rules above run;
- locked tests can't be edited (Edit/Write) or changed from the shell (`rm`, `mv`, `sed -i`, redirects, `git checkout/restore`, `Set-Content`, `Remove-Item`, …). Running them (`pytest tests/x.py`) is fine;
- while a build is active, `.claude/settings*.json` and `.claude/tq-rules.yaml` are locked too, so the gate can't be switched off from inside;
- Windows paths are normalized (`\` → `/`) before any match.

These are string checks, like tq's guard: they catch honest mistakes. What makes cheating pointless is the evidence check: `verify` hashes the locked tests before running anything and fails if one changed by any route.

**Stop:** for the session that owns the build (the one that launched its marker agents), a turn can't end while the active checkpoint has started but isn't proven, unless the plan has a top-level `halted` reason. It blocks **once**: Claude Code's `stop_hook_active` flag is honored, and the gate's own counter lets the next stop through, so a structurally broken plan or a confused session can never trap the user. Other sessions and the plugin's headless children are never blocked.

## Locked tests through tq (`.claude/tq-rules.yaml`)

tq's PreToolUse guard reads every `.claude/tq-rules.yaml` from the working directory up to the workspace root. A project file can only **add** rules (its `disable:` key is ignored), so a repository can't loosen the guard. `build-gate lock-tests` writes one:

```yaml
# Generated by tentaqles build-gate. tq's guard reads it; project rules can only add.
rules:
  - id: "tentaqles-build/locked-tests"
    action: "ask"            # or deny: lock-tests --action deny
    tool: "Edit|Write|MultiEdit|NotebookEdit"
    path: "(^|/)(tests/test_pricing\\.py|tests/api/test_discounts\\.py)$"
    reason: "locked test of a tentaqles build: the implementer must not edit it; report a wrong test instead"
```

- `tool` is anchored and `path` is matched against the file path with `\` normalized to `/`, so the same rule works on Windows and POSIX.
- `ask` makes Claude Code show the human a confirmation; `deny` refuses outright. The gate's own hook already denies these edits; the tq rule is a second, independent layer that stays in force even if the gate's hooks are inactive.
- If the project already has its own `tq-rules.yaml`, the rule is merged into it (other rules kept, backup in `.claude/build/`). `build-gate finish` removes it again.
- To write such a rule by hand for any project, use the same shape: an `id`, `action: ask|deny`, a `tool` regex, a `path` regex, a `reason`. See `docs/CLAUDE-HOOK.md` in the tq repo for every matcher (`command`, `content`, `except`, `existing_only`).

## Kit bugs this port fixes

| Kit behavior | Here |
|---|---|
| `docs/checkpoints/` regex never matched Windows backslash paths: plan rules silently off | paths normalized before matching |
| hook matcher `Agent|Task|Write|Edit|Bash`: PowerShell writes unchecked | PowerShell (and MultiEdit/NotebookEdit) covered, with PowerShell write verbs |
| Stop could block forever on a structurally invalid plan; relied on a non-existent `stop_reason` | `stop_hook_active` honored, at most one block, structural problems reported, downgrades always allowed |
| `open()` without encoding: cp1252 on Windows, crash = gate fails open | UTF-8 everywhere |
| `python -m http.server` on 0.0.0.0 serving the repo root (`.env`, auth state) | `build-gate serve`: 127.0.0.1, serves only the status render |
| critic had Edit/Write/Bash ("read-only" by prompt) | `build-critic` has Read/Glob/Grep only, the rest disallowed |
| commits without an identity check | tq's guard checks every commit; the skill never bypasses it (`-c user.email`, `--no-verify`) |
| `git switch -c` in a tree other sessions share | dedicated `git worktree` per build |
| gates passed on the model's word | evidence recorded by the gate, tied to the tree hash |
| `python3` (WindowsApps alias) in hook commands | hooks run through the plugin's `tq_run.sh` |

## Troubleshooting

- **`ping` says inactive:** hooks are read at session start. Open `/hooks` to approve the new ones or restart Claude Code in the worktree.
- **"code changed since the last passing verify":** expected after any edit. Run `build-gate verify <slug> <id>` again.
- **A verify command is wrong:** fix it in the plan, then `build-gate approve <slug>` (the human confirms). The frozen copy is what runs.
- **A test is wrong:** the implementer reports it; the human runs `build-gate unlock-tests <slug> <file>`, the test is fixed and locked again.
- **The plugin moved (upgrade):** run `build-gate setup` again; it rewrites the hook command with the new path.
