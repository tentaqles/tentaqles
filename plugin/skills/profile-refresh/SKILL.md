---
name: profile-refresh
description: Generate or refresh the learned workspace profile. Use when the user asks for a workspace profile, says "refresh profile", "workspace profile", "what are my hot files", "how active is this workspace", or triggers /tentaqles:profile.
triggers:
  - /tentaqles:profile
  - refresh profile
  - workspace profile
---

# Workspace Profile Refresh

Generate (or regenerate) the learned profile for the current workspace by
mining its memory database.

## Process

1. Resolve the workspace root from the manifest:

   ```python
   from tentaqles.manifest.loader import load_manifest
   import os

   manifest = load_manifest(os.getcwd())
   workspace_root = manifest.get("_client_root", os.getcwd()) if manifest else os.getcwd()
   ```

2. Open the memory store and instantiate the profiler:

   ```python
   from tentaqles.memory.store import MemoryStore
   from tentaqles.memory.profiler import WorkspaceProfiler

   store = MemoryStore(workspace_root)
   profiler = WorkspaceProfiler(store, workspace_root)
   ```

3. Generate the profile (always regenerates, regardless of staleness):

   ```python
   profile = profiler.generate()
   store.close()
   ```

4. Display the profile to the user in this format:

   ```
   ## Workspace Profile — {workspace}
   Generated: {generated_at[:10]}

   ### Session Frequency
   - Sessions last 30 days: {session_frequency.sessions_last_30d}
   - Avg sessions/week: {session_frequency.sessions_per_week_avg}
   - Most active hour (UTC): {session_frequency.most_active_hour}:00

   ### Hot Files (top {len(hot_files)})
   {for each hot file: "- {path} — score {score}, {touch_count} touches [{trend}]"}

   ### Top Concepts
   {for each concept: "- {label}: {decision_count} decisions — "{representative[:80]}...""}

   ### Commit Velocity (last 30 days)
   {if commit_velocity: "- {commits_30d} commits ({commits_per_week_avg}/week)"}
   {else: "- Not a git repo or git not available"}

   ---
   {summary_sentence}
   ```

5. If no sessions exist yet, tell the user:
   > "No memory recorded for this workspace yet. Run a few sessions first so
   > the profiler has data to mine."

## Notes

- The profile is cached at `{workspace}/.claude/profile.json` and considered
  stale after 7 days. `generate()` always writes a fresh copy.
- `session-preamble.py` (Wave 3) will call `profiler.load()` to inject the
  profile into the SessionStart context automatically.
- If the workspace is not a git repository, `commit_velocity` will be `null`
  — this is expected and safe.
