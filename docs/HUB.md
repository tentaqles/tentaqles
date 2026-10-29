# `tq hub` — a local board that runs agent-ready cards

`tq hub` keeps one board per client workspace and runs cards headless through the
plugin's gated build engine (`/tentaqles:kickoff`, `/tentaqles:feature`,
`/tentaqles:fix`, `/tentaqles:build`). Every human stop of a run becomes a decision
waiting on you; you answer it from the page, from `tq hub answer`, or in your terminal
with `build-gate answer`. Nothing leaves the machine, except what board sync sends to the
company tracker under its write policy (see [Board sync](#board-sync)).

## Quick start

```powershell
cd C:\work\acme                       # a trusted workspace
tq hub add "Export invoices" --kind feature --tag agent-ready --project apps/billing --body "CSV per month."
tq hub serve                          # http://127.0.0.1:7470/  (in its own terminal, see below)
tq hub move c-xxxxxxxxxxxx "in progress"   # an agent-ready card in an active lane starts a run
tq hub list --waiting                 # what the run waits on
tq hub answer c-xxxxxxxxxxxx d1a2b3c4 --choice approve
tq hub review c-xxxxxxxxxxxx --open   # evidence from the gate's records; Approve or Request changes
```

Run `tq hub serve` in your own terminal, not as an agent's background task: a Claude Code
session can reap its background tasks when memory runs low, and the hub goes with it.

## One workspace or all of them

| | `tq hub serve` | `tq hub serve --all` (or `hub: {mode: all}` in `$TQ_HOME/config.yaml`) |
|---|---|---|
| serves | the trusted workspace containing the current folder | every trusted workspace under the registered bases; untrusted ones are listed as unavailable and never opened |
| databases | one | one per workspace, opened on first use (those that already have a board open at start so their queues run) |
| pages | that board | a workspace switcher; `/waiting` lists every workspace's decisions, labelled |
| runs | that workspace's environment | each card's own workspace environment; concurrency per workspace |

The mode is fixed when the hub starts. A second hub on the same port stops with a
message naming the running one; another port works, but a workspace is served by one
live hub at a time: in single mode a second hub refuses a workspace another hub already
serves, and `--all` lists it as unavailable ("served by the hub on port N"), so two hubs
never run the same queue. `tq hub status` lists running hubs.

## Web access

Default bind `127.0.0.1:7470` (`--port`, `hub.port`). Any non-loopback `--bind` needs a
token: `--token T`, or one is generated and printed once (only its SHA-256 is stored); a
browser on another machine opens `/login?token=<token>` once.

The page is guarded against DNS rebinding and cross-site requests:

- The hub answers only requests whose Host is on its allow-list: the loopback names
  (`127.0.0.1`, `localhost`, `::1`) with the port, plus, on a non-loopback bind, the bound
  address (every interface address for a wildcard bind) and the machine's host name. A
  hub started with neither an allow-list nor a token refuses every request (it fails
  closed).
- Every POST must be same-origin: the `Origin` (else `Referer`) host is judged against
  that same allow-list, never against the request's own Host header. The token sent as
  a request header, or the local CLI's per-process key, stands in for the origin check.
- The live-update stream (SSE) sends ids only, and the page re-reads what changed
  through the guarded API. The one exception is the `log` frames of a run page, which
  carry the run log's text, already masked.

## The page

The button at the top right switches between dark and light (a sun while dark, a moon while
light); the choice is kept in this browser (`localStorage` key `tq-hub-theme`). With no choice
saved it follows the system, and is dark when the system states no preference.
It needs no network: no web fonts, no CDN; Inter and JetBrains Mono are used when installed.

| Page | What it is for |
|---|---|
| Board (`/`) | lanes with their cards; drag a card to move it, or use the Move button at a card's top right (Tab to it, Enter or Down opens the lane menu, arrows pick, Escape closes; focus stays on the card). Below 900px wide the sidebar folds into a bar above the page that keeps the workspace switcher. An agent-ready card that enters In progress starts a run; moving a running card out asks before cancelling the run. An orange edge means a run waits on you, with the action on the card. The box at the top filters cards by title (`/` focuses it). The Done lane shows its 20 most recently updated cards; its count is the full total, and "Show all N done" (`/?done=all`) lists every one. |
| New card (button in the top bar, `+` on every planning lane) | title, body (markdown; a `budget:` front-matter line overrides the default budget), kind, lane, project folder, parent card (a card of kind `project`), tags, and the agent-ready switch. |
| Card (`/cards/<id>`) | for a linked card, the tracker badge (`ADO #123`, `Jira ACME-7`, a link when the item has a URL) and the company lane; for a local card in a workspace with a tracker, **Push to company board** (`tq hub push`); the body, a timeline of runs, rounds, decisions and the card's outbox rows (sent, pending, waiting on you, skipped, failed), the live log of an active run, the run's facts and budget bar, the tag editor (Enter or comma adds, × or Backspace removes; each change saves; without script × and Add work as a form against the stored list), comments (Ctrl+Enter posts), and any decision waiting on you. |
| Decisions (`/waiting`) | every pending decision across the served workspaces, newest first, with the option buttons and a note; an over-budget stop shows the run's numbers, and its note takes new limits such as `rounds=6,usd=15`. |
| Runs (`/runs`), run (`/runs/<id>`) | runs in flight and the 50 most recent; a run's rounds, log, facts and budget. |
| Reviews (`/reviews`), review (`/cards/<id>/review`) | results waiting on your review and finished runs not yet closed; the review page opens with a verdict band built from the gate's records, and Approve / Request changes sit in a bar at the bottom. The exported review keeps the same look. |
| Activity (`/activity`) | spend, runs ended, median wait on you, decisions answered and cost by kind for Today, 7 days or 30 days (`?days=1\|7\|30`), and the timeline by day, filtered by `?kind=runs\|decisions\|cards\|reviews`. |
| Connections (`/connections`) | the workspace's company tracker (see [Board sync](#board-sync)): provider and scope, the auth source in use (or the preferred one before the first check) and the fixed fallback order, the signed-in account, last sync, last error, next poll, outbox counts, a **Sync now** button (checks the login, then runs one pass in the background), the lane map, the six write-policy toggles (`off` / `ask` / `auto`) and the poll settings (sync on/off, interval). Saving is human-only. Without a tracker block the page says how to add one (`tq onboard`, then `tq allow`). |
| Settings (`/settings`) | what this hub serves (read-only), concurrency (1 to 16), the default budget per run, desktop notifications, and who can do what. A value the form refuses shows the page again (status 400) with what you typed and the message next to the field; a save is all-or-nothing. Stop the hub with Ctrl+C in its terminal; the page has no stop button. |

The board shows a linked card's badge and its company lane on the card.

`POST /api/cards/<id>` edits a card (`title`, `body`, `project_dir`, `tags`; an unknown field is a 400); `GET` and
`POST /api/settings` read and change the settings (`concurrency`, `default_budget`,
`notify`, and `sync`: `{"enabled", "interval", "policy": {"<action>": "off|ask|auto"}}`).
Both follow the same rules as the page. `GET /api/sync` returns the sync overview,
`POST /api/sync/now` starts a pass (202), and `POST /api/cards/<id>/push` queues a push.

## Cards, lanes, runs

Lanes: `backlog`, `todo`, `in progress` (kind `active`), `review`, `done`. Kinds:
`project` → `/tentaqles:kickoff`, `feature` / `analysis` / `chore` → `/tentaqles:feature`,
`fix` → `/tentaqles:fix`, `hotfix` → `/tentaqles:fix --hotfix` (only this kind). The
prompt is the card's title and body; context comes from the project's `CLAUDE.md`, PRD
and skills. Once the build plan exists, the prompt is `/tentaqles:build resume <slug> --headless`.
Every prompt, on-ramp or resume, ends with the human's card comments posted since the
run's previous round (the start of the latest round of any run on the card; every
comment when the card has none yet), under "Notes from the human on this card": only
human comments, never agent or system ones, the newest 5, each cut to 2,000 characters
and quoted (`> ` on every line) under a header line naming its author and time.
So a comment you post on the card reaches the next round.

A run starts when an `agent-ready` card enters an active lane (moved there, or added
there by a human), on
`tq hub run <card>`, or when an answer resumes it. Wakeups coalesce; a card is checked
out by one run at a time.

Every round runs `claude -p --output-format stream-json --verbose` in the card's project folder with the
workspace's environment plan (as `tq run` builds it), `TENTAQLES_HEADLESS=1` and
`TQ_HUB_TASK={"hub","workspace","kind","external"}` and `TQ_MODE` set to the workspace's
`guard.headless_mode` (see [Guard mode of a run](#guard-mode-of-a-run)); never with a hub token,
never with `TENTAQLES_HEADLESS_CHILD`, never with `--dangerously-skip-permissions` unless the
manifest's `claude.permission_mode` is `bypass` and the workspace has a mode grant
(`tq allow --mode <ws> relaxed|custom`). The prompt goes to `claude` on stdin and
the tools a `permission` answer allowed go in a `--settings` file; nothing free-text is
put on the command line (a resumed round adds `--resume=<session id>`).
The hub reads the events as they arrive and writes one masked line per step to the run
log, which the card and run pages tail live: `→ tool Bash: go test ./...`,
`→ editing internal/x.go`, a subagent start, the first line of what Claude says (clipped
to 200 characters), and at most every 30 seconds the round's running cost and tokens.
The stream's final `result` event gives the cost, usage and session id; an output without
one is read as a single result document, as before.

When the build creates its worktree, the session ends with
`TQ_HUB_RELOCATE <worktree> <slug>` and the next round starts there. The hand-off is
accepted only for a folder inside the workspace root or the build's sibling worktree
`<project>-build-<slug>` next to the project folder; anything else fails the run
("relocate outside workspace"), and a second hand-off for the same slug fails it as a
relocate loop.

Two cards of one project folder may run at once, but the rounds before relocation run in
the project folder itself. So a run takes the folder's **pre-relocation lock** when its
first such round starts, and keeps it until it relocates or ends. Another run of the same
folder stays `queued` meanwhile (it does not use a concurrency slot) and its card gets one
comment naming the run it waits for. The lock is kept in memory and in the store, so a
second process sees it; on start the hub drops locks whose run ended, relocated or no
longer exists. After relocation each build has its own worktree and needs no lock.

After each round
the hub reads `build-gate state <slug> --json`: pending decisions → `waiting`; a halted
or rolled-back build → `failed`; a non-zero exit with no progress → one retry after two
minutes; a round that exits 0 with a final message before any build slug exists (the
feature, kickoff or fix step-1 stops, which have no gate record to write) → `waiting` on a
hub `question` (see Decisions); anything else that ends without a stop → `failed` with
the transcript tail.

Before every round, first or resumed, the hub re-checks at zero cost that the workspace
is still trusted (a skill that edits the manifest changes its hash), that the run's folder
exists, and that `git` is on PATH and the folder is inside a git repository (a card without
a project folder runs in the workspace root, so the root must be one). If a check fails, no
round starts: the run goes to `waiting` with a stop reason (`untrusted`, `project folder
missing`, `not a git repository`, or `git preflight` when git refused a folder for another
reason, such as "dubious ownership", whose fix is `safe.directory`; git's own message is
quoted), the card gets a comment, and a hub decision asks
"resume / stop". Fix the cause (for example `tq allow <ws>`), then answer resume. If a
round ends on a usage limit (the CLI says the account is out of usage credits or has hit
a usage, weekly or rate limit, including when the stream ends without a result event), the run waits the same way with stop reason `usage limit`. It
keeps the round's cost and does not count as a failure, and resume continues the session.

Statuses: `queued running waiting finished failed cancelled over-budget`.

Stopping the hub never cancels work. A run whose round was cut short goes back to
`queued` with its lock, the card gets a "the hub stopped during round N" comment, and the
run resumes when the hub starts again. A run still marked `running` by a hub that died
is failed on the next start ("the hub stopped during this round") and its lock released;
the gate state is kept, so `tq hub run <card>` resumes it. Only an explicit
`tq hub cancel <run>` (or Cancel on the page) ends a run as `cancelled`.

## Budgets

Per run: 2,000,000 tokens (input + output + cache write), 10 USD, 120 minutes, 12
rounds, or the card's `--budget tokens=…,usd=…,minutes=…,rounds=…`. Reaching one stops
the run (`over-budget`), comments the numbers and asks "raise-and-resume / stop".
Tokens, dollars and minutes are checked inside a round, not only between rounds: the hub
prices each assistant message as it streams and, once the round reaches what is left of
the budget, stops `claude` (as a cancel does), records what the round spent and stops the
run the same way. The session id is kept, so raise-and-resume continues that session.
A card body's `budget:` front matter is checked when the card is created or its body
edited: an unknown key or a value that is not a positive number (rounds and tokens whole)
is refused with an error naming it (`bad_request` on the API), so it never reaches a run.
A `budget` object on `POST /api/cards` must carry all four limits; a partial one is a 400.
A body edit is checked as a whole, so a card whose stored front matter already holds an
invalid `budget:` line takes a body edit only once that edit fixes the line.
When Claude Code reports no cost, it is computed from the usage and the model; when it
reports neither, the round is marked "cost unknown" and only rounds and minutes count it.

## Decisions

The hub mirrors the gate's decision records (`plan-approval`, `result`, `judge`,
`question`) and adds its own `permission` kind (a refused tool: `allow-once`; a budget
stop: `raise-and-resume`; a held run (untrusted, folder, git, usage limit): `resume`,
which requeues the same run).

| Kind | Owner | Asked when | Options |
|---|---|---|---|
| `plan-approval`, `result`, `judge`, `question` | the gate (mirrored) | the skill wrote a decision record | the record's own |
| `permission` | the hub | a refused tool, a budget stop, a held run | `allow-once` / `raise-and-resume` / `resume`, and `stop` |
| `question` | the hub | a round exited 0 with a final message and no build slug yet | `answer`, `stop` |
| `permission` (company board) | the sync engine | an outbox write under an `ask` policy | `send`, `skip` |

A company-board decision shows the redacted text that would leave the machine. Answer it
on the board or with `tq hub answer <card> <decision> --choice send|skip`; the sync sends
or skips the write on its next pass. Its note stays in the hub.

The hub's own `question` carries the agent's final message as its text and shows on
`/waiting` and the card like any other decision (the card gets the orange edge).
`answer` needs a note: it is posted as your comment on the card and the same run goes
back to the queue, continuing its session, so the next prompt carries your answer under
"Notes from the human on this card". `stop` ends the run as cancelled. A round that ends
without a stop after a slug exists still fails, and a non-zero exit still retries once.

Answering a gate decision from the hub mints a hub actor token
(HMAC-SHA256 with the gate key, actor `hub:<your user>`) for one `build-gate answer` call;
the gate validates it and applies the consequence. The hub never calls `approve`,
`finish`, `judge`, `dod --override` or `unlock-tests`. If you answered in the terminal
first, the hub notices on its next read and resumes.

## Agent sessions

A `tq hub` command run with `TENTAQLES_HEADLESS=1`, `CLAUDE_PLUGIN_ROOT`, `CLAUDECODE=1` or
`TQ_HUB_TASK` in its environment (or an API request with `X-TQ-Hub-Agent: 1`) counts as
an agent session. It may add cards, but only into backlog or todo lanes (or lanes of kind
`other`), comment, move cards between those lanes, and edit the tags of a card in one of
those lanes. It never starts a run (`tq hub run`,
a move into an active lane, or a card added straight into one), cancels one, answers a
decision, approves, requests changes, moves a card to review or done, edits a card's
title, body or project folder, changes the hub's settings, or sets a budget:
neither `--budget` nor a card body whose front matter has a `budget:` key is accepted from
an agent. It cannot start `tq hub serve --bind` on a non-loopback address either (that
hub's token must never reach an agent). The refusal tells you to run the command in your
own terminal or use the page. A comment from an agent session is always stored as an
agent comment, never a human one, so it never reaches a run's prompt as the human's
notes; only a human answers the hub's `question`.

For board sync, an agent session may run `tq hub sync now`, `status`, `outbox`, and the
MCP fallback commands `import`, `pending` and `ack` within their limits (see
[MCP fallback](#mcp-fallback)). `tq hub push`, `tq hub sync retry`, the Connections
settings and the send/skip decisions are human-only. `tq hub edit` from an agent may
only add or remove tags on a card in a planning lane.

Runs launch with their own workspace's environment only: whatever the shell hook set for
another workspace (recorded in `__TQ_STATE`) is undone and every provider config variable
(`CLAUDE_CONFIG_DIR`, `GH_CONFIG_DIR`, cloud config homes and the rest of the catalog) is
removed before the workspace's own are applied, so `tq hub serve --all` started from one
client's shell never hands another client's run that client's identity.

## Guard mode of a run

A run uses the workspace's `guard.headless_mode`, not its interactive mode:

```yaml
guard:
  mode: relaxed          # interactive sessions
  headless_mode: strict  # hub runs: default (when absent), strict, relaxed or custom
```

The runner sets `TQ_MODE` to that value for every round (`default` when it is
absent or not a stored mode) and drops a `TQ_MODE` or `TQ_ENABLED` inherited
from the shell that started the hub. A value looser than `default` still needs
the workspace's grant: `relaxed` for a run needs both
`headless_mode: relaxed` in the trusted manifest and `tq allow --mode <ws>
relaxed`. Without the grant the run is clamped to `default`. Runs are started by
tq, not by a tool call, so the nested-agent rule does not apply to the runner's
own launch. See `CLAUDE-HOOK.md`, "Guard modes".

The page `/guard` shows the resolved mode, the category table with each value's
source, the ceiling and the 24 h counts of skipped confirms and blocks,
read-only: "change it with `tq mode edit` or the desktop app". It has no form and
no POST route, because a web form is scriptable from an agent's `curl` and the hub
API has no proof of a human. It is reached by URL (`/guard`); the nav has no link
yet.

## Review page

`/cards/<id>/review` (or `tq hub review <card> --open|--html`): goal, approach (the plan
record, flagged when it changed since approval), what was done (commits since the base,
diff summary, PR link), where it stands (checkpoints, reviewer verdicts, definition of
done), evidence (verify records with command, exit code, tree and time), cost per round,
feedback (Request changes) and Approve. The page is built only from the gate's own
records (`build-gate state --json --records`) plus the hub's own `git log` and
`git diff --stat` of the worktree since the base commit; what the agent says about its
work never reaches it (its final text is kept as a card comment). On approval the page is exported to
`$TQ_HOME/hub/<workspace>/runs/<run>.review.html`.

## Board sync

A workspace whose manifest has a `tracker` block mirrors the company tracker into its
board: each tracker item becomes one linked card, and some hub events are written back.
The block holds names only (see [PROVIDERS.md](PROVIDERS.md#company-services-in-the-manifest)):

```yaml
tracker:
  provider: jira                # github | azure-devops | jira | linear | asana
  scope: { org: acme, project: PLAT }
  assigned_to: me               # me | anyone
  auth: keychain                # cli | keychain | env | mcp
  lanes: { "In Review": review }
```

This build ships background adapters for all five trackers: **GitHub** (Issues and
Projects v2), **Azure DevOps** Boards, **Jira Cloud**, **Linear** and **Asana**. Per-provider
scope keys are in [PROVIDERS.md](PROVIDERS.md#tracker-scope-per-provider). Without a
background credential the MCP fallback (`/tentaqles:sync`) is the way to sync.

### Polling

A serving hub (`tq hub serve`) runs one sync engine per workspace with a tracker block. It
starts and stops with that workspace's runner, does a full pass at start, then one every
`sync.interval` seconds (default 300, minimum 60). Between polls it sends the outbox every
15 s, and about 0.5 s after a hub event in the same process (an approval, say).
`sync.enabled=false` (Connections, Board sync off) stops the poll and the outbox; nothing is
lost. Errors are recorded as the last error (a repeated error is logged once).
`tq hub sync now` runs one pass without a hub. A lease in the store keeps a serving hub and
a CLI pass from sending the same row twice; a pass that finds it taken says "another tq
process is syncing this workspace right now".

### Inbound

- Items changed since the watermark are upserted into linked cards. The body is the item's
  description, plus a "## Acceptance" section when the tracker has acceptance criteria. A
  parent in the same batch is imported before its children.
- Lanes: a `tracker.lanes` override comes first (a lane name or a lane kind); otherwise the
  status category picks the first lane of that kind; an unmapped status goes to the
  backlog. The lane map is rebuilt from the tracker's statuses on the engine's first pass.
- An inbound move goes through the normal move with the actor `sync` and never starts a
  run: only a human move into an active lane starts one.
- Text: sync updates a linked card's title and body only while nobody has edited them here
  since sync last wrote them. A local edit is kept on every later pull, and queues
  `edit_text` (see below).
- Lane conflict: both sides moved since the item was last pulled (a local move counts if it
  came after that pull, or while an outbound move is still pending). The tracker wins: the
  card moves and a system comment records it. An outbound move whose item has changed on
  the tracker since is skipped with a comment.
- While a run holds a card, a move on the company board is not applied: sync comments once,
  keeps the old tracker status on the card, and makes the move on the first pull after the
  run releases the card. An approval during the run is checked against the old status, so
  it cannot reopen a closed item.

### Outbound: the outbox and its policy

Hub events queue outbox rows (history from before the engine's first pass is never
replayed):

| Event | Action |
|---|---|
| a human (not sync) moves a linked card into an active lane | `move_on_start` |
| a linked card moves into a done lane (unless an approval follows within 3 s) | `close_item` |
| you approve a linked card's result | `move_on_review` and `comment_results` (the result summary and the PR link) |
| you approve an unlinked child of a linked parent | `comment_results` on the parent's item |
| `tq hub push <card>`, or Push to company board | `create_card` (a child of the parent's item when the parent is linked) |
| anyone but sync edits a linked card's title or body (the edited fields, current text; a row not yet asked is replaced by the newer one) | `edit_text` |

Each action has a setting `sync.policy.<action>`:

- `auto` sends it;
- `ask` puts a `send` / `skip` decision on the board that shows the redacted text that
  would leave (see [Decisions](#decisions));
- `off` drops it (the row is `skipped`, last error "policy off: …").

Defaults: `move_on_start`, `move_on_review` and `comment_results` are `auto`;
`create_card`, `edit_text` and `close_item` are `ask`. Change them on the Connections page
or with `POST /api/settings`.

Several moves of one card queued during an outage are sent in order in one pass.

Retries:

- A rate-limited call is tried up to 3 times per pass, honouring `Retry-After` (else 1 s,
  then 2 s). A `Retry-After` over 60 s holds the row until then.
- Another transient failure waits 15 s, doubling up to 30 min, between tries. The row is
  marked `failed` only once it is 24 h old and has had at least 5 tries.
- Missing credentials, or an adapter that cannot be built (an offline CLI token fetch, for
  example), never count as a try; the row stays pending.
- Not-found and unsupported errors fail the row at once. `tq hub sync retry <id>` (human
  only) puts a failed row back to pending with its tries reset and sends it now.

Redaction: all outbound text is redacted with the board's mask classes (every detector
when the manifest has no `data` block; the block's classes plus secrets when it has one).
The `tq:allow-pii` marker is removed and exempts nothing on the way out. Tokens never
appear in payloads, logs or reports.

### Credentials

The engine resolves a credential for every pass, starting at `tracker.auth` (default
`cli`) and continuing in the fixed order cli → keychain → env → mcp (`auth: env` tries
env, then mcp):

- **cli**: the tracker CLI's own login, run with the workspace identity environment:
  `gh` for GitHub (under the identity's `GH_CONFIG_DIR`), `az account get-access-token
  --resource 499b84ac-1321-427f-aa17-267ca6975798` for Azure DevOps (under its
  `AZURE_CONFIG_DIR`) and `acli` for Jira. Linear and Asana have no CLI.
- **keychain**: the OS keychain entry `tq secret set <ws> <provider>` stores.
- **env**: the `tracker.env` variable, resolved as `tq dotenv run` does (the workspace
  identity env, then the process environment, then `<workspace root>/.env`).
- **mcp**: there is no background credential, so the engine reports `needs-session`; use
  `/tentaqles:sync` from a Claude session.

A token is fetched again for each request and never stored by tq. Errors name the source
that failed, never the token.

### MCP fallback

When the engine reports `needs-session`, the `/tentaqles:sync` skill does the same work
from a Claude session with the tracker's MCP connector:

- `tq hub sync import [-|<file>] [--json]` reads items as JSON (a bare array, or
  `{"items": [...], "statuses": [...], "watermark": "<RFC 3339>"}`). An item has `id`,
  `title` and optionally `key`, `url`, `body`, `status`, `status_category`, `parent_id`,
  `rev`, `acceptance`, `labels`. The watermark is the fallback's own cursor (shown as
  `fallback` in `tq hub sync status`), clamped to now; it never moves the engine's.
- `tq hub sync pending [--json]` queues new hub events, applies the policy, and lists the
  rows that may be sent now (`auto` rows, and `ask` rows the human answered `send`) with
  their redacted text. It never calls the tracker, and it refuses while a background
  credential works. Each listed row is claimed for 15 minutes, so the engine does not send
  it too.
- `tq hub sync ack <outbox-id> --sent|--failed "<why>" [--status S] [--item-id ID --url U]`
  records the outcome. For a move, `--status` names the item's status now; for
  `create_card`, `--item-id` and `--url` link the card to the new item (a link that fails
  leaves the row pending).

An agent session may import only into planning lanes (backlog, todo, other) and only
linked cards: a new item whose status maps elsewhere starts in the backlog, and a move into
or out of a non-planning lane, or a text change on a card outside one or held by a run, is
held (queued for the background sync, or for the human). An agent's statuses only fill
gaps in the lane map, never with a done lane. An agent may ack only `auto` rows or `ask`
rows the human approved, and never answers a decision.

### Commands

- `tq hub sync now [--json]`: one pass here; prints pulled / created / updated / moved /
  conflicts and queued / sent / asked / skipped / dropped / failed / retried.
- `tq hub sync status [--json]`: provider, scope, auth, on/off, interval, last ok and last
  error, next poll, outbox counts, policies and the lane map.
- `tq hub sync outbox [--status pending|sent|skipped|failed] [--card C] [--limit N] [--json]`.
- `tq hub sync retry <outbox-id> [--json]` (human only).
- `tq hub push <card> [--json]` (human only).

## Files and privacy

`$TQ_HOME/hub/<workspace>/hub.db` (SQLite, WAL), `runs/<run>.log`, `activity.jsonl`,
user-only permissions. Every stored text is masked with the workspace's data classes
(`data.classes` plus secrets; every detector when the workspace is not classified).
That covers card titles, bodies and tags, attachment labels and paths, comments,
decision text and notes, run logs and activity data.
`tq insights` reads `activity.jsonl` for its Hub section.

## Commands

```
tq hub serve [--all] [--port N] [--bind ADDR] [--token T] [--no-notify]
tq hub add "<title>" [--body TEXT|--body-file F] [--kind K] [--lane L] [--tag T]... [--parent ID] [--project DIR] [--budget …] [--json]
tq hub list [--lane L] [--kind K] [--tag T] [--waiting] [--json]
tq hub show <card> [--json]
tq hub edit <card> [--title T] [--body TEXT|--body-file F] [--project DIR] [--add-tag T]... [--remove-tag T]... [--json]
tq hub move <card> "<lane>" [--cancel-run]
tq hub run <card>
tq hub answer <card> <decision> --choice C [--note TEXT]
tq hub review <card> [--open] [--html]
tq hub open [<card>]
tq hub cancel <run>
tq hub comment <card|-> "<text>"        # - is the card in TQ_HUB_TASK
tq hub status
tq hub push <card> [--json]
tq hub sync now|status|outbox|retry|import|pending|ack …   # see Board sync
```

`tq hub edit` edits a card from the terminal (on the store, or over a running hub's
`POST /api/cards/<id>`). Tags are added and removed against the stored list, and adding
`agent-ready` starts nothing. It prints `edited <id>  <title>  (tags: …)`, or the card with
`--json`. Title, body and project folder are human-only.

All take `--workspace NAME` and `--timeout D` (skills pass `--timeout 2s`). They talk to
a running hub's API when one serves the workspace, else to the store directly.

## Troubleshooting

`tq doctor` reports these at level `info` (informational: they never change its exit
code), each with a fix:

- `hub-db-unreadable`: a workspace's `hub.db` cannot be opened or read (doctor opens it
  read-only and writes nothing).
- `hub-stale-lock`: a card locked by a run that ended or no longer exists, or by a run
  marked `running` while no hub serves the workspace.
- `hub-port-busy`: another program listens on the hub port (`hub.port`, default 7470);
  checked only when some workspace has a hub database.

Test seams, for tests only: `TQ_HUB_CLAUDE_CMD` and `TQ_HUB_GATE_CMD` (JSON argv) replace
`claude` and the plugin's `build-gate.py`; `TQ_HUB_PLUGIN_ROOT` and `TQ_BASH` override
plugin and bash discovery; the hidden `tq hub serve --drain` runs every queued run to its
next stop and exits (with `--all` it skips workspaces without a `hub.db`).
