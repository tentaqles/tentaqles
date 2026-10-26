# `tq hub` — a local board that runs agent-ready cards

`tq hub` keeps one board per client workspace and runs cards headless through the
plugin's gated build engine (`/tentaqles:kickoff`, `/tentaqles:feature`,
`/tentaqles:fix`, `/tentaqles:build`). Every human stop of a run becomes a decision
waiting on you; you answer it from the page, from `tq hub answer`, or in your terminal
with `build-gate answer`. Nothing leaves the machine.

## Quick start

```powershell
cd C:\work\acme                       # a trusted workspace
tq hub add "Export invoices" --kind feature --tag agent-ready --project apps/billing --body "CSV per month."
tq hub serve                          # http://127.0.0.1:7470/
tq hub move c-xxxxxxxxxxxx "in progress"   # an agent-ready card in an active lane starts a run
tq hub list --waiting                 # what the run waits on
tq hub answer c-xxxxxxxxxxxx d1a2b3c4 --choice approve
tq hub review c-xxxxxxxxxxxx --open   # evidence from the gate's records; Approve or Request changes
```

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
| Card (`/cards/<id>`) | the body, a timeline of runs, rounds and decisions, the live log of an active run, the run's facts and budget bar, the tag editor (Enter or comma adds, × or Backspace removes; each change saves; without script × and Add work as a form against the stored list), comments (Ctrl+Enter posts), and any decision waiting on you. |
| Decisions (`/waiting`) | every pending decision across the served workspaces, newest first, with the option buttons and a note; an over-budget stop shows the run's numbers, and its note takes new limits such as `rounds=6,usd=15`. |
| Runs (`/runs`), run (`/runs/<id>`) | runs in flight and the 50 most recent; a run's rounds, log, facts and budget. |
| Reviews (`/reviews`), review (`/cards/<id>/review`) | results waiting on your review and finished runs not yet closed; the review page opens with a verdict band built from the gate's records, and Approve / Request changes sit in a bar at the bottom. The exported review keeps the same look. |
| Activity (`/activity`) | spend, runs ended, median wait on you, decisions answered and cost by kind for Today, 7 days or 30 days (`?days=1\|7\|30`), and the timeline by day, filtered by `?kind=runs\|decisions\|cards\|reviews`. |
| Connections (`/connections`) | not available yet: board sync with Linear, Asana, Azure DevOps and Jira arrives in release 2 part B. The page shows what each will do and the planned lane mapping; it connects nothing. |
| Settings (`/settings`) | what this hub serves (read-only), concurrency (1 to 16), the default budget per run, desktop notifications, and who can do what. A value the form refuses shows the page again (status 400) with what you typed and the message next to the field; a save is all-or-nothing. Stop the hub with Ctrl+C in its terminal; the page has no stop button. |

`POST /api/cards/<id>` edits a card (`title`, `body`, `project_dir`, `tags`; an unknown field is a 400); `GET` and
`POST /api/settings` read and change the settings (`concurrency`, `default_budget`,
`notify`). Both follow the same rules as the page.

## Cards, lanes, runs

Lanes: `backlog`, `todo`, `in progress` (kind `active`), `review`, `done`. Kinds:
`project` → `/tentaqles:kickoff`, `feature` / `analysis` / `chore` → `/tentaqles:feature`,
`fix` → `/tentaqles:fix`, `hotfix` → `/tentaqles:fix --hotfix` (only this kind). The
prompt is the card's title and body; context comes from the project's `CLAUDE.md`, PRD
and skills. A run starts when an `agent-ready` card enters an active lane (moved there, or added
there by a human), on
`tq hub run <card>`, or when an answer resumes it. Wakeups coalesce; a card is checked
out by one run at a time.

Every round runs `claude -p --output-format json` in the card's project folder with the
workspace's environment plan (as `tq run` builds it), `TENTAQLES_HEADLESS=1` and
`TQ_HUB_TASK={"hub","workspace","kind","external"}`; never with a hub token, never with
`TENTAQLES_HEADLESS_CHILD`, never with `--dangerously-skip-permissions` unless
`tq allow --bypass` was run for the workspace. The prompt goes to `claude` on stdin and
the tools a `permission` answer allowed go in a `--settings` file; nothing free-text is
put on the command line (a resumed round adds `--resume=<session id>`).

When the build creates its worktree, the session ends with
`TQ_HUB_RELOCATE <worktree> <slug>` and the next round starts there. The hand-off is
accepted only for a folder inside the workspace root or the build's sibling worktree
`<project>-build-<slug>` next to the project folder; anything else fails the run
("relocate outside workspace"), and a second hand-off for the same slug fails it as a
relocate loop. After each round
the hub reads `build-gate state <slug> --json`: pending decisions → `waiting`; a halted
or rolled-back build → `failed`; a non-zero exit with no progress → one retry after two
minutes; anything else that ends without a stop → `failed` with the transcript tail.

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
A card body's `budget:` front matter is checked when the card is created or its body
edited: an unknown key or a value that is not a positive number (rounds and tokens whole)
is refused with an error naming it (`bad_request` on the API), so it never reaches a run.
When Claude Code reports no cost, it is computed from the usage and the model; when it
reports neither, the round is marked "cost unknown" and only rounds and minutes count it.

## Decisions

The hub mirrors the gate's decision records (`plan-approval`, `result`, `judge`,
`question`) and adds its own `permission` kind (a refused tool: `allow-once`; a budget
stop: `raise-and-resume`). Answering from the hub mints a hub actor token
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
own terminal or use the page.

Runs launch with their own workspace's environment only: whatever the shell hook set for
another workspace (recorded in `__TQ_STATE`) is undone and every provider config variable
(`CLAUDE_CONFIG_DIR`, `GH_CONFIG_DIR`, cloud config homes and the rest of the catalog) is
removed before the workspace's own are applied, so `tq hub serve --all` started from one
client's shell never hands another client's run that client's identity.

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
tq hub move <card> "<lane>" [--cancel-run]
tq hub run <card>
tq hub answer <card> <decision> --choice C [--note TEXT]
tq hub review <card> [--open] [--html]
tq hub open [<card>]
tq hub cancel <run>
tq hub comment <card|-> "<text>"        # - is the card in TQ_HUB_TASK
tq hub status
```

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
