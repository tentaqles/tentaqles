---
name: wrap
description: End-of-day (or end-of-session) wrap-up in one pass — summarizes what actually landed from git log and gh (commits, PRs opened and merged), lists open PRs with their CI and review state, saves decisions and pending items to Tentaqles memory through the session-wrap skill, and writes a short daily note. Use when the user says "wrap", "wrap up the day", "end of day", "EOD", "daily wrap-up", "daily note", "what did I do today", "close out the day", or "/wrap". For a quick memory save with no git/PR recap, session-wrap alone is enough.
---

# Wrap

One wrap-up instead of three: the day's recap, open PR/CI state, memory, and a
daily note. The memory part is **not** reimplemented here: this skill gathers
the facts, then runs the `session-wrap` skill to persist them.

Why it is layered this way: `session-wrap` is the memory primitive. It keeps
its own triggers ("done", "end session", "save session", recording a decision
mid-session), it is where skill corrections are recorded, and other parts of
the plugin point to it. It stays quick and offline. `wrap` adds the parts that
need git and the network on top, and calls it.

## 1. Scope

- **Window:** today from 00:00 local time by default. If the user says "this
  session", use the session's start time; if they give a date, use that day.
- **Repos:** the current git repo. When the cwd is a workspace root that
  holds several repos, include each repo directly under it that had activity.

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -c "
import os
from tentaqles.manifest.loader import load_manifest
m = load_manifest(os.getcwd())
print('root=' + (m['_client_root'] if m else os.getcwd()))
print('display=' + ((m.get('display_name') or m['client']) if m else 'Unknown Workspace'))
" 2>/dev/null || echo "root=$(pwd)"
```

Each Bash call starts a fresh shell, so variables don't survive from one block
to the next: type the resolved values (the root printed above, the window) into
every block that needs them.

```bash
SINCE="$(date +%Y-%m-%d) 00:00"     # or "2026-10-08 00:00", or the session start time
ROOT="<root from above>"
for d in "$ROOT" "$ROOT"/*/; do
  [ -e "$d/.git" ] || continue
  n=$(git -C "$d" log --all --since="$SINCE" --oneline 2>/dev/null | wc -l)
  dirty=$(git -C "$d" status --porcelain 2>/dev/null | wc -l)
  [ "$n" -gt 0 ] || [ "$dirty" -gt 0 ] && echo "$d commits=$n uncommitted=$dirty"
done
```

## 2. What was done

For each active repo, git and gh are the record of what shipped. The
conversation is only for intent and context.

```bash
cd "<repo>"
SINCE="$(date +%Y-%m-%d) 00:00"     # same window as step 1
DAY=$(date -u -d "$SINCE" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u +%Y-%m-%d)
git log --all --since="$SINCE" --author="$(git config user.email)" --date=short --pretty='%h %ad %s (%D)'
git status --short
gh pr list --author @me --state merged --search "merged:>=$DAY" --json number,title,url
gh pr list --author @me --state all --search "updated:>=$DAY" --json number,title,url,state
```

GitHub search runs in UTC, hence the UTC timestamp (stock macOS `date` has no
`-d`; it falls back to the UTC date, so check PRs merged near midnight by hand).
The merged-PR list is the authoritative record: squash merges made on GitHub
may carry a different author email than the local `user.email`, so `git log`
is a supplement for unmerged work.

Classify each item:

- **Landed:** merged PRs, commits on the default branch.
- **In review:** open PRs.
- **In progress:** commits only on a local or unpushed branch, uncommitted changes.

Collapse commits that belong to the same PR. Never call something done that no
merged PR or default-branch commit confirms; if the conversation says "done"
but nothing landed, it is "finished, pending review/merge".

## 3. Open PRs and CI

```bash
gh pr list --author @me --state open \
  --json number,title,url,isDraft,reviewDecision,statusCheckRollup \
  -q '.[] | "#\(.number) \(.title) | review: \(if (.reviewDecision // "") == "" then "none" else .reviewDecision end) | checks: \([.statusCheckRollup[]? | ((.conclusion // "") as $c | if $c != "" then $c else (.state // .status) end)] | group_by(.) | map("\(.[0])=\(length)") | join(" ")) | \(.url)"'
```

Report each open PR as green, red, pending or no checks, plus its review state.
State only what this output shows. A red PR or unanswered review becomes a
pending item in the next step; offer to run `babysit-pr` on it.

## 4. Save to memory (via session-wrap)

Read the open items first so you don't save duplicates:

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

echo '{"cwd": "{root}", "event": "context", "data": {}}' | "$TENTAQLES_PY" "${CLAUDE_PLUGIN_ROOT}/scripts/memory-bridge.py"
```

Then follow the **`session-wrap`** skill (its "Record to Memory" steps and
skill-correction detection) with what you gathered:

- **Summary:** the landed and in-review items, 1-3 sentences.
- **Decisions:** the ones made today that are not already in memory (the Stop
  hook records some automatically; don't save them twice).
- **Pending:** open PRs that are red or have unresolved review, uncommitted
  or unpushed work, and the "next" items from the conversation. Close the
  loop on items that are now done instead of adding new ones.
- **Touches:** the files that were the focus of the day.

If the memory bridge fails, keep going and include the unsaved summary,
decisions and pending items in the daily note so nothing is lost.

## 5. Daily note

Write a short note, readable in under a minute:

```markdown
# <YYYY-MM-DD> — <workspace display name>

## Landed
- <outcome, plain language> (<PR link>)

## In review
- <PR title> — checks: <green|red|pending>, review: <state> (<link>)

## In progress
- <what, where it stands>

## Decisions
- <chosen> — <why>

## Next
- <first thing to pick up tomorrow>
```

Rules: outcomes over file lists; no secrets, tokens or connection strings; no
business figures (revenue, order counts, customer data), only engineering
numbers; drop empty sections.

Save it to `{root}/.claude/wraps/<YYYY-MM-DD>.md` (append a `## Later` block
if the file already exists), but only when that path is git-ignored or the
root is not a git repo:

```bash
if ! git -C "{root}" rev-parse --git-dir >/dev/null 2>&1 || git -C "{root}" check-ignore -q .claude/wraps/note.md; then
  echo "ok to save in {root}/.claude/wraps"
else
  echo "would be tracked by git: use the scratchpad"
fi
```

Otherwise save it to your scratchpad directory and say where. Never commit it.

Client-specific formats (a team-channel message in a set voice, a timesheet)
belong in a client-local skill that starts from this note, not here.

## 6. Report

```
Wrapped <display name> for <date>:
  Landed: <N> PRs / <N> commits    In review: <N> (<green/red/pending>)
  In progress: <N>                 Memory: <N> decisions, <N> pending saved
  Note: <path>
```

Then show the note. Offer to run `babysit-pr` on any red PR.
