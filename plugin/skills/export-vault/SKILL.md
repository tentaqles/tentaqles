---
name: export-vault
description: Refresh the Obsidian mirror of this workspace's memory (context, decisions, plan, fixes, session notes) and ingest the configured Markdown folders into memory search. Use when the user says "export vault", "export to obsidian", "refresh the vault", "sync notes", "update my notes", or asks to see memory in Obsidian.
---

# Export Vault

Write this workspace's memory as Obsidian notes under `<vault_dir>/<client>/` and pull any configured Markdown folders (for example a Clippings dir) into memory search. The SessionEnd hook does this automatically; this skill is the manual refresh.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/vault-export.py" --cwd "$(pwd)"
```

Pass `--no-ingest` to only rewrite the notes.

Report what the script printed: how many notes were written and where, and how many reference documents were added or updated. If it says the export is not configured, show the user the two ways to turn it on:

- shared, for every identity: `~/.tentaqles/knowledge.yaml` with `vault_dir: <folder>` and, per client, `ingest: {<client>: [<folders>]}`;
- per workspace: a `knowledge:` section in `.tentaqles.yaml` (`vault_dir`, `export_sessions`, `export_fixes`, `ingest_dirs`). Because the manifest travels with the repository, its paths are confined: `vault_dir` must be inside the vault the user configured and `ingest_dirs` inside the workspace; anything else is ignored. Changing the manifest changes its trust hash, so `tq allow <client>` is needed afterwards.

Notes under `sessions/` are written once and never rewritten; `context.md`, `decisions.md`, `plan.md` and `fixes.md` are regenerated on every export, so edits belong in Obsidian notes that link to them, not in these files.
