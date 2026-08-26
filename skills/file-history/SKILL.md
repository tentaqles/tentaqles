---
name: file-history
description: Show the full temporal history of a file in Tentaqles memory — every session that touched it, what action was taken, and any related decisions. Use when the user asks about a file's history, wants to know when/why/how a file was changed, says "what's the history of", "who touched", "when did we change", "why is X the way it is", or needs to understand the story behind a piece of code.
---

# File History

Show all Tentaqles records about the file at `$ARGUMENTS`.

## Process

1. If `$ARGUMENTS` is empty, ask the user which file they want the history for.
2. Run the file history script:

   ```bash
   # Load tentaqles runtime
   _tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

   "$TENTAQLES_PY" "${CLAUDE_PLUGIN_ROOT}/scripts/file_history.py" "$ARGUMENTS"
   ```

3. Present the output to the user. Highlight:
   - The number of touches and the time range (first and most recent)
   - Any high-weight touches (debug sessions, creates)
   - Related decisions — these explain _why_ changes were made
   - If no history is found, explain that the file hasn't been recorded in
     this workspace's memory yet (either it's new or predates memory tracking)

4. If the user has followup questions, you can:
   - Query specific sessions via `/tentaqles-query-memory`
   - Check related nodes via the graph if one exists
   - Open the file to see current state

## What this skill is good for

- Debugging a regression: "when did this file start behaving differently?"
- Onboarding to unfamiliar code: "what's the story of this module?"
- Understanding a decision: "why is auth.py using RS256?"
- Finding co-evolved files: "what other files usually change with this one?"
