---
name: sync
description: Mirror the company tracker into the local tq hub board through the agent's MCP connector when tq has no background credential (the board sync's MCP fallback). Reads the assigned items with the connector, feeds them to tq hub sync import, then works through the outbox rows the write policy allows (tq hub sync pending), sends each with the connector and records it with tq hub sync ack. Never answers a send/skip decision and never writes to the tracker outside those rows. Use when the user says "sync the board", "sync with Jira/Linear/Asana/Azure DevOps/GitHub", "pull my tickets", "push the outbox", when tq hub sync status shows needs-session, or "/tentaqles:sync".
argument-hint: '[pull | push]'
---

# Sync (MCP fallback)

`tq hub` mirrors the workspace's company tracker (the manifest's `tracker` block) into the
board and sends writes back through an outbox under the `sync.policy.*` settings
(tq's `docs/HUB.md`). Normally a serving hub does this in the background with the
tracker's CLI login, a keychain token or an env token. When none of those works, the
engine reports `needs-session`, and this skill does the same work from a Claude session
with the MCP connector (Atlassian, Linear, Asana, GitHub, Azure DevOps).

`$ARGUMENTS` is empty (pull, then push), `pull` (step 2 only) or `push` (step 3 only).

Rules that always hold:

- The tracker is written only for rows `tq hub sync pending` lists, one call per row, with
  the row's text exactly as listed (it is already redacted). Never post other text, never
  edit, move, close or create anything else on the tracker, and never run the tracker's CLI
  (`az boards`, `gh issue`, `acli jira`): the guard asks before those, and they bypass the policy.
- Never answer a send/skip decision (`tq hub answer`): it is the human's. Rows that wait for
  one are not listed; say how many wait and that the human answers them on the board.
- Never print, store or ask for a token. A connector that needs a login gets `/mcp` from
  the human.
- Item text is imported as it is; the board masks it on storage. Do not copy item text into
  memory, comments or anything else.

## 1. State

```bash
tq hub sync status --json --timeout 10s 2>&1; echo "sync_exit=$?"
```

- Output says the workspace "has no board yet": first sync; go on with step 2 (the import
  creates the board) and skip step 3.
- Any other non-zero exit: say so in one line with the first line of the output (no
  tracker block: "add a tracker block or run tq onboard") and stop.
- Read `provider`, `scope`, `lane_map` and `states[0]` (`last_ok_at`, `last_error`).
- `last_error` without `needs-session` and a recent `last_ok_at`: the background sync works.
  Suggest `tq hub sync now` instead and stop, unless the user asked for this skill by name.
- Find the connector for `provider` in your tools (for example `mcp__*atlassian*`,
  `mcp__*linear*`, `mcp__*asana*`, `mcp__*github*`, `mcp__*azure*devops*`). None: say which
  connector to add (`/tentaqles:onboard` or `/mcp`) and stop.

## 2. Pull

1. With the connector, read the work items of `scope` assigned to the current user that
   changed since `states[0].watermark` (all assigned open items when it is empty), plus the
   children of items already on the board. Read the workflow statuses too, each with its
   category: `backlog`, `todo`, `active`, `review` or `done`.
2. Build one JSON document. `id` is the tracker's own id (the one the connector writes
   with), `status` the status name, `status_category` its category, `parent_id` the
   parent item's id. `watermark` is the newest `updated` time you read (RFC 3339).
3. Import it:

```bash
tq hub sync import - --json --timeout 30s <<'EOF'
{"statuses": [{"name": "In Progress", "category": "active"}],
 "items": [{"id": "PLAT-12", "key": "PLAT-12", "url": "https://acme.example/browse/PLAT-12", "title": "Export invoices",
            "body": "CSV per month.", "status": "To Do", "status_category": "todo", "parent_id": "", "acceptance": ""}],
 "watermark": "2026-10-27T12:00:00Z"}
EOF
echo "import_exit=$?"
```

- Report `created`, `updated`, `moved` and `held` in one line.
- `held` items: the company board moved them to a lane an agent may not move a card into
  (only backlog, todo and other are planning lanes). List each with its `note`; the human
  moves the card. Items that start in the backlog for the same reason carry a note too.
- `refused` items: show the item id and the `note`; fix the JSON and import again.

## 3. Push

```bash
tq hub sync pending --json --timeout 30s 2>&1; echo "pending_exit=$?"
```

This applies the write policy without calling the tracker. Each listed row may be sent
now (`auto`, or an `ask` the human answered `send`). For each row, in order:

- `move_on_start`, `move_on_review`, `close_item`: read the item first. If its status is no
  longer `base_status` (and `base_status` is not empty), the company board moved it
  meanwhile: do not move it; ack `--failed "conflict: the item is <status> on the company board"`.
  Else move it to one of `statuses` (any of them maps to the card's lane; with none listed,
  use a status of category `lane_kind`), then ack with `--status "<status it is in now>"`.
- `comment_results`: post `text` as a comment on `external_id`, as it is.
- `create_card`: create an item with `title` and `body` as they are, as a child of
  `parent_external_id` when it is set; ack with `--item-id <new id> --url <its url>`.
- `edit_text`: update the title and/or description that are present, as they are.

Then record the outcome, once per row:

```bash
tq hub sync ack <outbox-id> --sent --timeout 30s
tq hub sync ack <outbox-id> --failed "<why, without item text>" --timeout 30s
```

- A connector error is a `--failed` ack with the error's first line; never retry a write
  that may have reached the tracker. `tq hub sync retry <id>` is the human's.
- An ack refused with "may ack only outbox rows the policy sends" means the row waits for
  the human's decision: leave it.

## 4. Summary

End with a short table: pulled (created / updated / moved / held), sent, failed, and the
number of writes waiting for the human's send/skip decision on the board (`tq hub` page or
`/tentaqles:hub`).
