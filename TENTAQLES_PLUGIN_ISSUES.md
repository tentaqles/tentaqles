# Tentaqles Plugin — Issues Observed During session-wrap

Captured 2026-04-15 while running `/tentaqles:session-wrap` from Claude Code in
`C:\repos\dirtybird\dbi-database` on Windows (Git Bash shell, plugin
`tentaqles/0.3.0`). Filing so the issues can be addressed in the tentaqles
plugin repo.

## 1. `CLAUDE_PLUGIN_ROOT` is not exported at skill invocation

**Symptom**

```
>>> python -c "from tentaqles.manifest.loader import load_manifest; ..."
err=No module named 'tentaqles'
```

**Root cause**

The skill body (`skills/session-wrap/SKILL.md`) uses:

```python
sys.path.insert(0, os.environ.get('CLAUDE_PLUGIN_ROOT', '.'))
from tentaqles.manifest.loader import load_manifest
```

`CLAUDE_PLUGIN_ROOT` is not set in the shell environment when the skill runs —
so `sys.path` gets prepended with `.` (cwd) and the import fails. Any user
invoking this skill from a cwd that isn't the plugin root hits this.

**Workaround used**

I had to manually hardcode the plugin path when running the snippet:

```
CLAUDE_PLUGIN_ROOT="C:/Users/renato/.claude/plugins/cache/tentaqles/tentaqles/0.3.0" \
  python -c "..."
```

**Suggested fix**

Either:
- The harness exports `CLAUDE_PLUGIN_ROOT` for every skill subprocess; OR
- The skill body computes the plugin root itself via
  `Path(__file__).resolve().parents[2]` (since it knows it lives at
  `skills/session-wrap/SKILL.md`); OR
- Ship a tiny wrapper (`tentaqles-python` or similar) that sets `sys.path`
  before dispatching.

## 2. `session_end` event errors with "no active session"

**Symptom**

```
>>> echo '{"event":"session_end", ...}' | python memory-bridge.py
{"error": "no active session"}
```

**Root cause**

`memory-bridge.py`'s `session_end` handler requires a matching
`session_start` record to exist for the current cwd. The `session-wrap` skill
calls `session_end` directly without ever calling `session_start` — so on a
first-run or orphaned session it always fails.

Every subsequent event in the same bash invocation (`decision`, `pending`,
`touch`) succeeded, so only the top-level summary is lost — the worst-affected
output is the "Last session ... Session with no file changes" line, which is
wrong but harmless-looking.

**Suggested fix**

Either:
- `session_start` should auto-fire on first activity in a cwd (lazy session
  creation); OR
- `session_end` should upsert rather than require a pre-existing session; OR
- The skill body should call `session_start` (if missing) before
  `session_end`; OR
- Add an explicit `session_upsert` event for the wrap-up skill.

## 3. `python` shim in broken venvs silently hijacks skill invocation

**Symptom**

```
>>> python scripts/...
No Python at '"C:\Users\renat\anaconda3\python.exe'
```

**Root cause**

Not a tentaqles bug per se, but relevant: when the user has a `.venv/Scripts/`
on PATH that was created against an anaconda interpreter that no longer exists
(or has a typo'd home path in `pyvenv.cfg`), `python` in bash resolves to the
broken shim and every plugin script blows up with an opaque message.

The memory-bridge script is fine, but the skill body writes `python` bare in
its bash snippets — so the call fails before ever reaching the bridge.

**Follow-up observation (2026-04-15): missing deps in hijacked venv**

Even when the venv's `python` shim *does* resolve, it's still the wrong
interpreter for the plugin. Observed in `C:\repos\dirtybird\dbi-database`:

```
>>> python scripts/memory-bridge.py
ModuleNotFoundError: No module named 'numpy'
```

The project venv doesn't (and shouldn't) carry the plugin's runtime deps
(`numpy`, embedding libs, etc.). So "bare `python`" isn't just a Windows-shim
hazard — any project venv that lacks the plugin's deps will break the bridge.
The plugin must not share the caller's Python environment.

**Suggested fix**

- Use `py -3` (Windows Python launcher) or probe `sys.executable` at plugin
  install time and pin the interpreter path in the plugin config.
- Alternatively, plugin scripts should shebang to `#!/usr/bin/env python3`
  and the skill body should invoke them directly (bash resolves the shebang),
  avoiding a bare `python` call.
- **Stronger fix**: ship the plugin with its own isolated interpreter /
  managed venv (e.g. `~/.claude/plugins/cache/tentaqles/.venv/`) created at
  install time with the plugin's `requirements.txt`, and have all skills
  invoke that interpreter by absolute path. This decouples the plugin
  entirely from whatever the caller's cwd venv looks like.

## 4. Minor: `touch` events return no stdout

**Observation**

`touch` events write nothing to stdout on success. Easy to mistake for a
silent failure. The `context` query after the fact confirmed the touches had
landed. A one-liner ack (`{"touch_id": "..."}`) would match the style of
`decision` and `pending` responses.

## What survived despite the above

During this wrap-up, 4 decisions + 6 pending items + 5 file touches were
successfully persisted to workspace `C:/repos/dirtybird` (client=dirtybird).
Only the session-end summary line was lost. A follow-up `context` call
returned the correct hot-node list and pending queue.

## Environment

- Plugin: `tentaqles/0.3.0`
- Path: `C:\Users\renato\.claude\plugins\cache\tentaqles\tentaqles\0.3.0\`
- OS: Windows 11, Git Bash under Claude Code CLI
- Python actually used: `C:/Users/renato/AppData/Local/Programs/Python/Python312/python.exe`
- Broken venv python: `C:/repos/dirtybird/dbi-database/.venv/Scripts/python.exe`
  pointing at non-existent `C:/Users/renat/anaconda3` (note typo'd home)
