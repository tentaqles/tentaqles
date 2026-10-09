---
name: loop
description: Overnight improvement loop (Karpathy style) for one feature with a clear test score. During the day you write and approve locked checks; at night a runner works in its own git worktree, one fresh headless claude round at a time, keeping a change only when the score improves and undoing it otherwise, inside hard budgets (dollars, tokens, hours, rounds) with MCP and network off. Results are waiting in the morning. Use when the user says "/tentaqles:loop", "run this overnight", "loop on feature X until the tests pass", or asks for the loop's results.
argument-hint: <approve|run|status> <feature> [options]
---

# Loop (overnight, test-gated)

A loop is worth it only when all four hold: the task repeats; the usage budget can absorb failed rounds; a score checks it without a human (locked tests); and the agent can run what it built. One feature per loop, never a whole app. If one of the four fails, suggest `/tentaqles:build` or a plain session instead.

Every Bash block starts with this prefix (it finds the plugin and its Python); use the Bash tool, not PowerShell:

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/loop-runner.py" status --feature <feature>
```

Below, `loop-runner <cmd>` means that second line with `<cmd>`.

## Day: write and approve the checks (human present)

1. **Name** the feature in kebab-case (`guest-ordering`). Write its rules in plain words into `FEATURES.md` (create it if missing): one section per feature, rules only.
2. **Write the checks** into `checks/` (e.g. `checks/test_guest_ordering.py`, `checks/guest-ordering.test.ts`), before the feature exists. One check per rule plus the usual ways this kind of feature breaks, each named as a plain sentence. Read the project's config first; use its test runner (pytest, vitest, jest, go test, …). Don't install a runner without asking.
3. **Show the human** every check as one plain line and ask: *"Happy with these? Say yes, or tell me what to change."* Iterate until yes. This is the only question the loop ever asks.
4. **Commit the checks** (explicit paths: `git add checks/<file> FEATURES.md`, then `git commit`). Uncommitted checks can't be approved.
5. **Approve** (freezes the checks, the scorer command, the expected test count and the test names). The human runs it in their own terminal: it needs an interactive terminal and a typed one-time code, and the approval is signed with a key outside the repo, so an approval file written by an agent is rejected. Print the exact command with absolute paths for them:
   ```
   loop-runner approve --feature <feature> --checks checks/<file> \
     --score-cmd "{python} -m pytest checks/<file> -q -p no:cacheprovider --junitxml={xml}" \
     --spec-file FEATURES.md
   ```
   - `{xml}` is where the runner reads JUnit XML; `{python}` resolves to the project's venv (`.venv/Scripts/python.exe` on Windows, `.venv/bin/python`, `uv run python` for uv projects).
   - JS: `--score-cmd "npx vitest run checks/<file> --reporter=junit --outputFile={xml}"`. Any runner that writes JUnit XML works; a custom scorer that prints `score: X/Y` works with `--kind stdout`.
   - If the baseline finds no tests (a crash before the feature exists is normal), pass `--expect <number of checks>`.
   - `--lock <glob>` adds locked paths. Locked by default: the checks, every `conftest.py`, `pytest.ini`, `pyproject.toml`, `setup.cfg`, `tox.ini`, `package.json` and lockfiles, `vitest/vite/jest/playwright` configs, `tsconfig*.json`, `.github/**`, `.claude/**`. The loop can't add dependencies or change how tests are collected.
   - `--notes "<how to work>"` passes standing advice to every round.
6. Tell the user the command for tonight (next section), with budgets filled in.

## Night: run (unattended)

```
loop-runner run --feature <feature> --workspace <tq workspace> \
  --budget-usd 10 --per-round-usd 2 --max-tokens 5000000 --max-hours 6 \
  --max-rounds 10 --round-timeout 1800 --score-timeout 600 --signal
```

Run it from the repo's main checkout, in a terminal, or schedule it (Windows Task Scheduler, cron). What it does:

- **Preflight:** `tq run <workspace> -- git config user.email` (an untrusted or broken workspace stops the run before anything happens) and `tq doctor` (warnings noted). Checks the approved files exist at the approved commit.
- **Worktree:** `git worktree add ../<repo>-loop-<feature>-<run> -b loop/<feature>-<run>` at the approved commit. Your checkout is never touched.
- **Each round** is one fresh `claude -p` call through `tq run <workspace> -- <claude full path>`, so it runs under that workspace's `CLAUDE_CONFIG_DIR` and identity. Flags: `--permission-mode dontAsk` with an allowlist of `Read, Edit, Write, Glob, Grep, Bash`; `--strict-mcp-config` with an empty MCP config (no MCP servers); `--max-budget-usd` capped at what's left; `--settings` with deny rules and a PreToolUse guard on every tool call. The guard blocks edits to locked paths and outside the worktree, any git command that changes history, network and package-manager commands, `.env` reads and environment dumps. Network tools only with `--allow-network`.
- **Integrity, checked by the runner after every round:**
  - the guard config, settings, MCP config and the plugin's guard code are hashed before the first round, and any change aborts the run (`config-tamper`). The guard also refuses to work when its config no longer matches the hash pinned in the child's environment;
  - HEAD must still be the runner's last kept commit;
  - no locked path may change, including new ignored files such as a planted `.claude/settings.local.json` or `sitecustomize.py`;
  - the child never loads project or local settings from the worktree (`--setting-sources user`), gets `--disallowedTools` for the locked and runner paths, and can't write outside the worktree except temp files.
- **Scoring is the runner's job.** After each round it checks the locked paths against the approved commit (any change: undo, logged `tamper`), runs the scorer with a timeout (process tree killed on timeout), and rejects a changed test count. Better score: commit the changed files **by name** (never `git add -A`; `.env*`, keys, `*.pem`, files over 1 MB and build by-products are skipped; tq's commit secret scan runs first). Otherwise: `git reset --hard` + `git clean -fd`, only after confirming the worktree is on the loop branch.
- **Budgets:** it stops before a round when dollars, tokens (input + output + cache writes), wall-clock time or rounds run out, or when the score is full. A round whose cost can't be read counts as the full per-round cap.
- **Morning:** `.claude/loop/runs/<run>/summary.md` (+ `summary.json`, `results.tsv`), a copy at `.claude/loop/LATEST-<feature>.md`, and with `--signal` a tentaqles signal (`loop <feature>: 12/12 (done) in 6 rounds, …`) that shows in the next session preamble. Run state lives in `.claude/loop/`, git-excluded.

The loop never pushes, never opens a PR and never merges.

## Morning: review

When the user asks for results: `loop-runner status --feature <feature>`. Summarize the score, rounds kept/undone, spend, stop reason and notes (tamper attempts, skipped files). Then have the branch reviewed like any other change before merging: the checks prove the rules, not that the feature is wired into the app. Suggest `/tentaqles:build` or a review pass for the wiring, and `tq worktrees prune` to clean up merged loop worktrees.

## Never

- Never edit `checks/` or any locked path to make a score pass, and never re-approve to loosen checks without the human.
- Never run the loop in the main checkout, and never on `main`/`master`.
- Never read or print `.env` files; commands that need them use `tq dotenv run -- <command>`.
