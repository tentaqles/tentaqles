---
name: rollback
description: Roll back the current workspace manifest (.tentaqles.yaml) to a previous snapshot. Use when the user says "rollback", "restore manifest", "undo identity switch", "undo manifest change", or wants to revert to a previous workspace state. Snapshots capture the full manifest before every identity switch and manifest edit.
triggers:
  - rollback
  - restore manifest
  - undo identity switch
---

# Rollback — Restore a Previous Workspace Manifest

Restore the workspace `.tentaqles.yaml` to a previously captured snapshot.

**Important:** Snapshots restore your identity and config state only. They do NOT undo memory.db changes (session history, decisions, touches).

## Process

1. Determine the workspace path. If not obvious from context, use the current working directory or ask the user.

2. List available snapshots:

   ```bash
   # Load tentaqles runtime
   _tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

   "$TENTAQLES_PY" "${CLAUDE_PLUGIN_ROOT}/scripts/snapshot.py" list "<workspace_path>"
   ```

   The output is a JSON array sorted newest-first. Each entry has:
   - `captured_at` — ISO timestamp when the snapshot was taken
   - `reason` — why it was captured (`auto-switch`, `manifest-edit`, `manual`)
   - `manifest_hash` — SHA-256 of the manifest at capture time
   - `path` — full path to the snapshot file

3. Present the list to the user in a readable table (timestamp, reason, hash prefix). Ask them to pick which snapshot to restore. If there are no snapshots, inform the user and stop.

4. Once the user picks a snapshot, get its manifest dict:

   ```bash
   # Load tentaqles runtime
   _tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

   "$TENTAQLES_PY" "${CLAUDE_PLUGIN_ROOT}/scripts/snapshot.py" restore "<workspace_path>" "<timestamp_prefix>"
   ```

   Use the `captured_at` value (or the filename stem) as the `<timestamp_prefix>`.

5. The command outputs the manifest as JSON. Parse it and write it back to `.tentaqles.yaml` in YAML format. The file lives at `<workspace_path>/.tentaqles.yaml`.

   Write the file using the standard Write tool. This will also trigger a new snapshot via the snapshot-guard hook (preserving the rollback action in history).

6. Confirm to the user: "Restored manifest from snapshot `<captured_at>` (reason: `<reason>`). The workspace is now using the previous identity configuration."

7. Recommend restarting the Claude Code session so the restored manifest takes effect in the preamble.

## Notes

- If the user wants to preview what changed before restoring, you can compare two snapshots manually (read both JSON files and diff the `manifest` keys).
- The `auto-switch` reason means an identity switch triggered the snapshot automatically — this is the most common restore target.
- The `manifest-edit` reason means a Write to `.tentaqles.yaml` triggered the snapshot — useful for undoing accidental edits.
