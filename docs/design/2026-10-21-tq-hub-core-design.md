# `tq hub` core — design (release 2, spec A)

**Status:** draft for user review, 2026-10-21.
**Parent:** `2026-10-20-development-process-design.md` §13, release 2. This is the first of two release-2 specs: **A, hub core** (this document) and **B, board sync** (tracker adapters and provider onboarding, written after A ships).
**Inputs:** the parent spec, `tentaqles/docs/IDEATION.md` (the developer's board, agent-ready cards, decisions on cards, the review page), the Paperclip design notes kept in the session scratchpad (product stance and status vocabulary; Paperclip is MIT and a reference only, nothing copied), and the release-1 engine as merged into `main` (gate decision records, hub actor tokens, headless mode, `build-gate state --json`).

## 1. Decisions already taken

| Question | Decision |
|---|---|
| Scope cut | Two specs. A: board, runs, headless adapter, decisions, review page, CLI, web page, activity log. B: tracker sync and onboarding. A is usable on its own with local cards. (User, 2026-10-21.) |
| Topology | Both modes behind a switch: `tq hub serve` serves the client workspace it runs in; `tq hub serve --all` serves every registered, trusted workspace from one process with a workspace switcher. (User, 2026-10-21.) |
| Language and shape | Go, inside the `tq` binary (`internal/hub`, `tq hub …`). Pure-Go SQLite driver, no cgo. Web page from embedded Go templates plus small plain JavaScript; no npm, no build step. |
| Isolation | One database per client workspace, outside the repository. The hub process holds no client credentials in either mode; each run is launched with the workspace's own environment the way `tq run` builds it. No cross-client data in one table, ever. |
| Authority | The human stays the only authority. The hub acts as the human's terminal: a click or a `tq hub answer` mints a hub actor token with the gate key and calls the gate. The hub never writes gate state and never runs a skip-permissions agent. |
| Masking | Every text the hub stores (card bodies, comments, decision text, review page content, activity) passes `tq redact` with the workspace's data classes first, like memory. |
| Reference, not fork | Paperclip's product stance is adopted: every screen answers *what is happening, does it need me, what do I do*. Its code is not used. |

## 2. Goals, non-goals, success criteria

**Goals.** A developer working for a client can: keep a local board of that client's work; tag a card agent-ready and have the release-1 engine run it headless in the right project with the right client settings; see every decision an agent is waiting on in one list and answer it from the page, the CLI, or the terminal; review a finished task on one page with its evidence and either send feedback or approve; see what every run cost; and trust that a run cannot bypass a human stop, a locked test, or a data guard because it was started by the hub rather than by a person.

**Non-goals for spec A.** Tracker sync and provider onboarding (spec B). The desktop GUI (Wails) and phone notifications. The Codex adapter (the adapter contract is fixed here; a plain-process adapter ships for tests). Multi-user access control: the hub serves one developer on one machine. Running the hub as a system service.

**Success criteria.**
- A card moved to "in progress" with the `agent-ready` tag produces a run whose first stop appears as a pending decision on the card within one runner tick, and answering it from the page resumes the run; the gate's own `decisions.jsonl` shows the answer with actor `hub:<user>`.
- A finished run yields a review page whose evidence section is built only from gate records (verify runs, reviewer verdicts, DoD), never from agent text.
- A run that reaches any budget stops with status `over-budget`, a card comment, and no further process started.
- `tq hub serve --all` on a machine with three trusted workspaces shows three boards and one combined waiting list, and `--all` with an untrusted workspace lists it as unavailable without opening its database.
- The hub binary never reads a `.env` file or a secret store; a run's environment is produced by the existing environment plan at launch.
- The whole Go test suite stays green without cgo and without a real `claude` binary (a fake adapter and a fake `claude` script cover the runner).

## 3. Principles

1. **Records, not prose.** The hub shows state it read from the gate or produced itself. An agent's summary is displayed as a comment, never as status.
2. **The human is the actor.** Every mutation that the gate treats as human-only is triggered by a human action on the hub and signed by the hub on their behalf. There is no automatic approval.
3. **Fail closed, stop quietly.** A run that cannot proceed (budget, permission, failure) stops and leaves a decision or a comment. It never retries into a loop.
4. **Local first.** Localhost bind, local SQLite, local logs. Anything beyond the machine is spec B or later.
5. **One name per concept.** Card, lane, run, decision, review, activity. Not issue, ticket, job, approval request.
6. **Hooks never stall** ([[hooks-must-never-stall]] applies): the plugin only contacts the hub with sub-second bounded calls.

## 4. Architecture

```
tq hub serve ─┬─ web (templates + SSE) ─── browser: board · card · waiting · review · runs · activity
              ├─ api (same handlers, JSON)
              ├─ store (SQLite per workspace) ─── $TQ_HOME/hub/<ws>/hub.db, runs/<run>.log, activity.jsonl
              ├─ runner (queue, budgets, concurrency) ─── adapter: claude | process
              └─ gatecli (build-gate state/answer via tq_run.sh, hub token)   ─── the project's .claude/build/<slug>/
tq hub add|list|show|move|run|answer|review|open|cancel|comment  ─── talk to the store directly, or to a running serve over HTTP
```

**Packages.** `internal/hub/store` (schema, migrations, queries), `internal/hub/runner` (queue, run lifecycle, budgets), `internal/hub/adapter` (contract, `claude`, `process`), `internal/hub/gate` (gate command invocation and token minting), `internal/hub/web` (handlers, templates, static), `internal/hub/activity` (append-only log and the insights reader), `internal/cmd/hub.go` (CLI). Templates and static files are embedded with `embed`.

**Storage locations.** `$TQ_HOME/hub/<workspace>/hub.db` (SQLite, WAL mode), `$TQ_HOME/hub/<workspace>/runs/<run-id>.log` (adapter stdout and stderr, masked), `$TQ_HOME/hub/<workspace>/activity.jsonl` (append-only, masked; `tq insights` reads it). Nothing is written inside a client repository except what the engine already writes.

**Dependencies added to `go.mod`.** `modernc.org/sqlite` (pure Go). Nothing else; HTTP, templates, SSE and HMAC come from the standard library.

**Ports.** Default `127.0.0.1:7470`. A second `tq hub serve` on the same port exits with a message naming the running hub and suggesting `--all` or `--port`. The gate's own live view (`build-gate serve`, port 8765) is unchanged; the card page links to it when a run is active.

## 5. The topology switch

| | `tq hub serve` (default) | `tq hub serve --all` or `hub.mode: all` in `$TQ_HOME/config.yaml` |
|---|---|---|
| Workspaces served | the one whose folder contains the cwd (trusted) | every workspace under the registered bases that is trusted; untrusted ones are listed as unavailable and never opened |
| Databases open | one | one per served workspace, opened lazily on first request |
| Pages | board of that workspace | workspace switcher in the header; `/waiting` combines all workspaces, each row labelled |
| Runs | launched for that workspace | launched for the card's workspace, with that workspace's environment; concurrency limits apply per workspace |
| Credentials in the hub process | none | none |

Both modes use the same handlers with a workspace parameter. Mode is decided once at start; changing it means restarting the hub.

## 6. Data model

All `id` columns are 12-character random base32 strings prefixed by the kind (`c-`, `r-`, `d-`). Timestamps are UTC RFC 3339.

**cards** — `id`, `title`, `body` (markdown, masked), `lane`, `kind` (`project`, `feature`, `fix`, `analysis`, `hotfix`, `chore`), `tags` (JSON array; `agent-ready` is the only tag with behaviour), `parent_id` (nullable), `project_dir` (path relative to the workspace root; the run's cwd), `external_provider`, `external_id`, `external_url`, `external_lane` (all nullable, written by spec B), `locked_by_run` (nullable), `created_at`, `updated_at`, `done_at`.

**comments** — `id`, `card_id`, `author_kind` (`human`, `agent`, `system`), `author` (user name or `hub`), `body` (masked), `run_id` (nullable), `created_at`.

**decisions** — `id` (the gate's decision id), `card_id`, `run_id`, `slug`, `kind` (`plan-approval`, `result`, `judge`, `question`, `permission`), `checkpoint` (nullable), `reviewer` (nullable), `text` (masked), `options` (JSON), `status` (`pending`, `answered`), `choice`, `note`, `actor`, `asked_at`, `answered_at`. Rows mirror the gate's `decisions.jsonl`; `permission` is the hub's own kind (see §8.6) and has no gate record.

**attachments** — `id`, `card_id`, `path` (absolute or workspace-relative; the hub stores the reference, never a copy), `label`, `created_at`.

**runs** — `id`, `card_id`, `workspace`, `adapter` (`claude`, `process`), `on_ramp` (`kickoff`, `feature`, `fix`, `build`, `resume`), `project_dir`, `worktree` (nullable, read from gate state), `slug` (nullable until the on-ramp names it), `session_id` (adapter session for resume), `status` (`queued`, `running`, `waiting`, `finished`, `failed`, `cancelled`, `over-budget`), `budget` (JSON: `tokens`, `usd`, `minutes`, `rounds`), `usage` (JSON: input, output, cache read, cache write), `cost_usd`, `rounds`, `started_at`, `ended_at`, `log_path`, `state` (JSON: last `build-gate state --json`), `stop_reason` (text).

**lanes** — `name`, `position`, `kind` (`backlog`, `todo`, `active`, `review`, `done`, `other`). Default rows: backlog, todo, in progress (`active`), review, done. The `active` and `review` kinds carry behaviour (run trigger, review page); names are free.

**activity** — `ts`, `workspace`, `actor`, `verb` (`card.create`, `card.move`, `card.update`, `comment.add`, `run.start`, `run.wait`, `run.end`, `decision.ask`, `decision.answer`, `review.approve`, `review.feedback`, `budget.stop`), `card_id`, `run_id`, `data` (JSON, masked). Also appended to `activity.jsonl` as one line per row.

**settings** — key, value: `concurrency` (default 2), `default_budget` (default tokens 2,000,000, usd 10, minutes 120, rounds 12), `notify` (true), `web_token_hash` (set only for non-localhost binds).

## 7. Cards and lanes

- `tq hub add "<title>" --kind feature --lane backlog --tag agent-ready --project apps/billing --parent c-…` creates a card; the page has the same form. The body is markdown; a card for a project may carry the PRD path in its body.
- A child card lives on this board only. Pushing it to the company board is spec B; the columns exist so B needs no migration.
- Moving a card is `tq hub move <id> "<lane>"`, a drag on the page, or an API call. Moving an `agent-ready` card into the `active` lane enqueues a run (§8.1). Moving it out of `active` while a run is queued cancels the queued run; while running, it asks for confirmation on the page (the CLI needs `--cancel-run`).
- A card in `review` with a finished run shows the review page link; approving moves it to `done` and comments the outcome on the parent card ("<title>: approved, PR <url>").
- When every child of a card is `done`, the parent gets a system comment and is moved to `review` (not `done`: the parent's own review is the human's).

## 8. Run lifecycle

### 8.1 Trigger and checkout

A run is enqueued when (a) a card with the `agent-ready` tag enters a lane of kind `active`, (b) `tq hub run <card>` is called, or (c) a decision is answered and the run must resume. Checkout is one SQL statement: `UPDATE cards SET locked_by_run = ? WHERE id = ? AND locked_by_run IS NULL`; zero rows means another run owns the card and the request is dropped with a system comment. Duplicate wakeups for the same card coalesce into one queued run. The lock is released when the run ends in any status.

### 8.2 Picking the on-ramp

| Card kind | Prompt the adapter receives |
|---|---|
| `project` | `/tentaqles:kickoff <body> --headless` |
| `feature`, `analysis`, `chore` | `/tentaqles:feature <body> --headless` |
| `fix` | `/tentaqles:fix <body> --headless` |
| `hotfix` | `/tentaqles:fix <body> --hotfix --headless` (only this kind passes `--hotfix`; the hub never infers it) |
| resume (any kind) | `/tentaqles:build resume <slug> --headless` |

The card's title and body are the task text. The hub adds nothing else to the prompt; context comes from the project's `CLAUDE.md`, the PRD and the skills.

### 8.3 The adapter contract

```go
type Adapter interface {
    // Start launches one round in dir with env, writes stdout/stderr to log, and returns
    // when the process exits. It never blocks on a prompt.
    Start(ctx context.Context, req RunRequest, log io.Writer) (RunResult, error)
}
type RunRequest struct { Workspace, Dir, Prompt, SessionID string; Env []string; AllowedTools []string }
type RunResult struct { ExitCode int; SessionID string; Usage Usage; CostUSD float64; StopReason string; Transcript string }
```

**`claude` adapter.** Command: `claude -p <prompt> --output-format json [--resume <session>] [--permission-mode <mode from manifest>] [--allowedTools …]`, cwd = the card's project folder for the first round and the run's `worktree` once the hand-off of §13.4 has named it, environment = the workspace's environment plan plus `TENTAQLES_HEADLESS=1`, `TENTAQLES_HEADLESS_CHILD` **unset** (the plugin's own hooks must run), `TQ_HUB_TASK=<json>` (§13.1), and `TQ_BUILD_ACTOR`/`TQ_BUILD_TOKEN` **unset** (a run never carries a hub token). Permission mode is `claudeArgs` from `tq run`: the manifest's mode, `bypass` only when `tq allow --bypass` was granted, never injected by the hub. The JSON result gives `session_id`, `usage`, `total_cost_usd`, `num_turns`, `subtype` (`success`, `error_max_turns`, `error_during_execution`) and the final text; the hub stores them on the run and writes the final text as an agent comment (masked). When `total_cost_usd` is absent the cost is computed with `internal/insights.CostUSD` from usage and the model named in the output.

**`process` adapter.** Runs an arbitrary command with the same environment; used by tests and for scripted tasks. It has no session and no usage.

### 8.4 Outcome classification

After each round the hub reads `build-gate state <slug> --json` from the project folder (through the plugin's `tq_run.sh build-gate.py`, found via the workspace's Claude configuration; if the slug is not yet known the hub lists `.claude/build/*/` newer than the run start and takes the one whose plan `task.hub` is this card).

| Observation | Run status | Card effect |
|---|---|---|
| the final text ends with `TQ_HUB_RELOCATE <worktree> <slug>` (§13.4) | `running`, next round immediately with cwd = worktree | run gains `worktree` and `slug`; counts as a round, not as a retry |
| `pending_decisions` non-empty | `waiting` | decisions mirrored; card shows "waiting on you"; notification |
| `finished` true, `dod.ok` true (or `dod` null for a plain `build`) | `finished` | card to `review`; review page built |
| `halted` set | `failed` with `stop_reason` from `halted.reason` | system comment with the last findings |
| `rolled_back` true | `failed`, `stop_reason: rolled back` | system comment: re-approve in the terminal; the hub mints no token for this slug until the state is re-approved |
| process exit non-zero and state unchanged | one retry after 2 minutes if `rounds` allows, else `failed` | system comment with the last log lines |
| `subtype` is `error_max_turns` | treated as a round boundary: resume if budget allows | |
| none of the above and the process exited | `failed`, `stop_reason: ended without a stop` | the transcript tail as a comment |

### 8.5 Budgets, rounds, concurrency, cancel

A round is one adapter start. Budgets are per run and inherited from the card (`budget` field in the body front matter, optional) or the workspace default. Before each round the runner checks remaining tokens, dollars, minutes and rounds; after each round it adds the result's usage. Exceeding any limit sets `over-budget`, posts a system comment with the numbers, releases the lock, and raises a `permission`-kind decision "raise budget and resume / stop" (§8.6). Concurrency is a per-workspace limit on running adapters (default 2); queued runs start in FIFO order. `tq hub cancel <run>` sends the process the platform's termination signal, waits 10 s, kills, and sets `cancelled`; the gate state stays as it is, so a later `tq hub run` resumes from it.

### 8.6 Permission asks and hub-level decisions

Headless Claude Code cannot answer a permission prompt; a tool it may not use fails and the run usually ends with `error_during_execution` and a transcript naming the tool. The hub turns that into a decision of kind `permission` on the card with options `allow-once`, `stop`. `allow-once` resumes with `--allowedTools <tool spec>` for that round only; nothing is written to any settings file. The same kind carries the over-budget question. These decisions are the hub's own; they are not gate records and need no token.

### 8.7 Notifications

When a run enters `waiting` or ends in `failed` or `over-budget`, and `notify` is on, the hub shows one desktop notification (Windows: PowerShell toast through `tq`'s existing shell helper; macOS: `osascript`; Linux: `notify-send`), bounded to two seconds and never retried. The page's waiting list is the source of truth.

## 9. Decisions and approvals

### 9.1 Mirroring

After each round the hub copies the gate's `decisions` list into `decisions`, keyed by the gate's id, and marks answered ones. Pending gate decisions appear on the card and in `/waiting`. A decision carries what the gate wrote: for a `plan-approval`, the plain summary and the checkpoints; for a `result`, what to try and the checkpoint summary; for a `judge`, the reviewer's U rows; for a `question`, the text.

### 9.2 Answering

From the page (a button per option plus a note field), from `tq hub answer <card> <decision> --choice <option> [--note …]`, or in the terminal with the gate's own `build-gate answer` (the hub notices on the next state read). The hub path:

1. Refuse if the run's last state has `rolled_back` true, or the slug is unknown, or the choice is not one of the record's options.
2. Mint the token exactly as the gate does: `HMAC-SHA256(key, json(["hub", action, slug, actor]))` with the compact ASCII JSON the gate uses, key read from `$TQ_HOME/build-gate/hmac.key`, hex digest; `action` is `answer`, `actor` is `hub:<os user name>`.
3. Run `build-gate answer <slug> <id> --choice <choice> --note <note> --actor hub:<user>` in the project folder with `TQ_BUILD_ACTOR` and `TQ_BUILD_TOKEN` set only for that process. The gate validates the token and applies the consequence (an approved `result` runs `finish`; an `override` answer runs the DoD override; a `judge` records the verdict).
4. Mirror the answered record, log `decision.answer`, and enqueue a resume run unless the answer ends the run (`stop`, or `result` `approve`).

The hub never calls `approve`, `finish`, `judge` or `dod --override` directly; everything goes through `answer` on a record the engine asked for, so the engine's own rules about what each answer means stay in one place. `unlock-tests` is not exposed by the hub.

### 9.3 Who can click

Spec A serves one developer on localhost. Any non-localhost bind requires `--token` (or generates one and prints it once); the page sends it as a header and the hub compares against a stored hash. POST requests also need the same-origin header check. There is no user model; the actor name is the OS user.

## 10. Review page

`GET /cards/<id>/review` renders, for the latest finished run:

1. **Goal** — card title and body, parent card, kind, spec slice path (from gate state `feature` and the plan's `spec`).
2. **Approach** — the approved plan's summary and checkpoints with their test layers (from the plan JSON the gate signed).
3. **What was done** — commits since the run's base (`git log` in the worktree or project folder), diff summary by file, PR link if `ship` posted one.
4. **Where it stands** — per checkpoint: behaviour gate, review gate, reviewer verdicts, retries; the DoD record (`ok`, items, override and its reason).
5. **Evidence** — verify records (command, exit code, tree hash, time) and the review bases; all read from gate records.
6. **Cost** — rounds, tokens, dollars, wall time, per round.
7. **Feedback** — a text box and a "Request changes" button: posts a human comment, answers the pending `result` record with `request-changes` and the note (through §9.2), and the run resumes into the engine's request-changes path.
8. **Approve** — answers the `result` record with `approve` (the gate runs `finish`), moves the card to `done`, comments the outcome on the parent.

If the run has no pending `result` record (for example a `build` plan that finished without one), the page shows the evidence and only offers "Mark done" (a board action, no gate call). The page is also exported as a static HTML file next to the run log on approval, so the record survives the hub.

## 11. Web page

Pages: `/` board (lanes as columns, cards with kind, tags, waiting badge, running badge, cost so far), `/cards/<id>` (body, comments, decisions with answer forms, runs, attachments, links to the gate live view), `/waiting` (every pending decision across served workspaces, newest first, with answer forms inline), `/cards/<id>/review`, `/runs/<id>` (status, budget vs usage, log tail streamed), `/activity`. A header shows the workspace (or the switcher in `--all` mode) and the count of items waiting.

Live updates use one SSE stream per page (`/events?ws=…`), fed by the store's change notifications; pages re-render affected fragments with plain JavaScript, no framework. Status vocabulary is one token set used everywhere: `queued`, `running`, `waiting`, `finished`, `failed`, `cancelled`, `over-budget`. Machine values (ids, costs, token counts, times) use the monospace style. Light and dark follow the viewer's system setting. All templates escape by default; markdown in card bodies is rendered with a small safe subset (headings, lists, code, links) and no raw HTML.

## 12. CLI

```
tq hub serve [--all] [--port N] [--bind ADDR] [--token T] [--no-notify]
tq hub add "<title>" [--body TEXT|--body-file F] [--kind K] [--lane L] [--tag T]... [--parent ID] [--project DIR] [--budget tokens=…,usd=…,minutes=…,rounds=…]
tq hub list [--lane L] [--kind K] [--tag T] [--waiting] [--json]
tq hub show <card> [--json]
tq hub move <card> "<lane>" [--cancel-run]
tq hub run <card>                       # enqueue (or resume) a run now
tq hub answer <card> <decision> --choice C [--note TEXT]
tq hub review <card> [--open]           # prints the review URL or opens the browser
tq hub open [<card>]                    # opens the board or a card in the browser
tq hub cancel <run>
tq hub comment <card> "<text>"          # used by skills (ship posts the PR link)
tq hub status                           # running hub, mode, port, counts
```

Every command finds the workspace from the cwd (or `--workspace NAME`), requires it to be trusted, and talks to the store directly when no hub is serving, or to the running hub's API when one is (so the page updates live). Output is plain text, `--json` where listed.

## 13. Plugin changes

### 13.1 The `task` block

The on-ramps (`kickoff`, `feature`, `fix`) read `TQ_HUB_TASK` when set. It is JSON: `{"hub": "<card id>", "workspace": "<name>", "kind": "<card kind>", "external": null}` (spec B fills `external` with `{provider, id, url}`). The on-ramp passes it to the planner, which writes it verbatim as the plan's top-level `task`. The build skill already forbids inventing one; the gate already exposes it in `state --json`. The gate's plan schema accepts `task` as an object with those keys; a `task` present without `TQ_HUB_TASK` in the environment is a planner error the on-ramp rejects.

### 13.2 Headless hygiene

`TENTAQLES_HEADLESS=1` stays the switch the skills already honour. The hub does not set `TENTAQLES_HEADLESS_CHILD`, because the session hooks (memory, guard, gate) must run for a hub-launched session as for an interactive one. The Stop hook's behaviour with a pending decision is unchanged.

### 13.3 New and changed skills

- **`/tentaqles:hub`** (new): lists the workspace's pending decisions by calling `tq hub list --waiting --json` (bounded 2 s; if no hub data exists it says so), shows each in plain language, and when the user picks an answer in conversation runs nothing itself: it prints the exact `tq hub answer …` line for the user to run, following the human-only rule. It can also add cards (`tq hub add`) and show a card.
- **`ship`**: after opening the PR, if `TQ_HUB_TASK` is set, posts the PR URL with `tq hub comment` (bounded 2 s, failure ignored).
- **Session preamble**: if a hub database exists for the workspace, prints "N decisions waiting on you (tq hub open)" from a direct SQLite read bounded to 300 ms; never contacts the network.
- **`build` skill**: resume path unchanged; the `task` block sentence gains the field names above; Phase 0 gains the headless hand-off below.

### 13.4 Headless hand-off into the worktree

Phase 0 of the build skill creates the worktree, installs the gate hooks there, and then needs a session that *starts* in the worktree, because Claude Code reads hooks at session start. Interactively it tells the user to reopen Claude there. Headless, it ends the turn with a final line the hub can parse: `TQ_HUB_RELOCATE <absolute worktree path> <slug>`. The hub stores both on the run and starts the next round with cwd = worktree and the same prompt as a resume (`/tentaqles:build resume <slug> --headless`, or the on-ramp's own resume form). The skill writes this line only when `TENTAQLES_HEADLESS=1`; a run that prints it twice for the same slug is `failed` with `stop_reason: relocate loop`.

### 13.5 `tq doctor`

New checks: `hub-db-unreadable` (database exists but cannot be opened), `hub-stale-lock` (a card locked by a run that is not running), `hub-port-busy` (another process on the configured port). All informational with a fix line.

## 14. Activity and insights

Every mutation appends one masked line to `activity.jsonl` and one row to `activity`. `tq insights` gains a "Hub" section: cards per lane, runs started and ended by status, cost per card and per kind, median time a decision waited for the human, overrides and request-changes counts, budget stops. The reader lives in `internal/hub/activity` and is called from `internal/insights`, filtered by workspace like the existing readers.

## 15. Security model

- **Trust boundary.** The hub is a convenience in front of the gate. Everything that must be true without the hub stays true with it: human-only commands need a token the gate verifies; a rolled-back state invalidates tokens; locked tests, frozen commands and the DoD are unchanged. If the hub is compromised, an attacker with the same OS user could already run the gate's commands; the hub adds no new capability.
- **The key.** The hub reads `$TQ_HOME/build-gate/hmac.key` only inside the answer path (§9.2) and holds it in memory only for that call. The caveat from release 1 stands: until tq isolates the key at the OS level, protection is against a careless agent. The hub does not change that; the follow-up is still open.
- **Runs.** No skip-permissions unless the workspace was explicitly allowed with `tq allow --bypass`. `TQ_BUILD_*` never reach a run. The run's environment is the workspace's environment plan; the hub passes no secrets of its own.
- **Network.** Localhost by default; token plus origin check otherwise. No TLS in spec A (a LAN bind is for a trusted home network and the phone shortcut later; the token is the protection).
- **Data.** Masking at every write with the workspace's classes; the hub database and logs are per workspace under the tq home folder with user-only permissions; `--all` never joins data across workspaces in one query.
- **Agent writes to the hub.** Only `tq hub comment` and `tq hub add` are expected from an agent session, and both go through masking. The agent cannot move a card to `done`, answer a decision, or change budgets: those commands refuse when `TENTAQLES_HEADLESS=1` or `CLAUDE_PLUGIN_ROOT` is in the environment, the same way the gate's hook refuses human-only subcommands.

## 16. Testing strategy

- **Store:** schema, migrations, atomic checkout under concurrent goroutines, lane behaviour, parent completion rule, masking at write.
- **Runner:** budgets (each limit), rounds, concurrency, cancel, retry once, outcome classification table driven from fixture `state --json` documents, coalesced wakeups.
- **Adapter:** the `claude` adapter against a fake `claude` script (Python in `testdata`, platform-neutral) that emits the JSON result shapes (`success`, `error_max_turns`, `error_during_execution`, missing cost) and writes fixture gate state; the `process` adapter against `go run` helpers.
- **Gate path:** the token minting is tested against vectors produced by the Python `mint_hub_token` and committed as fixtures, so Go and Python stay byte-identical; the answer path runs against a fake `build-gate.py` that checks `TQ_BUILD_TOKEN`.
- **Web:** handler tests for every page and the API, CSRF and token checks, SSE delivery, markdown rendering safety.
- **Plugin:** tests for the `task` block round trip (env → planner → plan → `state --json`), the `hub` skill's frontmatter and bash blocks, the preamble's bounded read, and `ship`'s comment call.
- **Integration (`test/integration`):** a temporary workspace with a trusted manifest, a fake `claude`, `tq hub add` → move to `in progress` → waiting decision → `tq hub answer` → finished → review page assertions.

## 17. Assumptions and open points

Assumptions (correct any that are wrong):
- The developer runs one hub at a time per machine; `--all` is the way to see several clients. Two hubs on different ports are allowed but not coordinated.
- A card's `project_dir` is a folder inside the client workspace that already has the plugin's project block (`CLAUDE.md` `tq-project`); the on-ramps create it for `project` cards (kickoff) and require it otherwise.
- `claude -p --output-format json` keeps the `session_id`, `usage`, `total_cost_usd` and `subtype` fields; the adapter tolerates missing cost and missing usage.
- Desktop notifications are best effort; no notification channel is part of the trust model.
- Spec B will map tracker lanes onto the `lanes.kind` column and fill the `external_*` card columns; nothing in A is expected to change for that.

Open points for review:
- Whether `analysis` and `chore` cards should run through `feature` (proposal: yes; an analysis card's acceptance is a document, and the feature on-ramp handles a spec without code checkpoints) or get an on-ramp of their own later.
- Default concurrency 2 and default budget (2 M tokens, 10 USD, 120 min, 12 rounds): confirm or change.
- Whether the review page's approve button should also require a typed confirmation like the terminal's one-time code (proposal: no; the click on a localhost page is the human, and the gate still validates the token).

## 18. Provenance

Parent spec §13 release 2 (decided 2026-10-20); `IDEATION.md` (the developer's board, agent-ready cards, decisions annotated on cards, the review page with feedback and approve); Paperclip design notes (product stance, status vocabulary, "one name per concept"), MIT, reference only; release-1 code as merged: `plugin/tentaqles/workflow/gate.py` (`HUB_ACTIONS`, `mint_hub_token`, `cmd_state`, decision records), `plugin/skills/build/SKILL.md` (headless mode), `internal/cmd/run.go` (`claudeArgs`, environment plan), `internal/insights` (pricing), `internal/registry` (bases). User decisions of 2026-10-21: two specs; both topologies on a switch.

## 19. Rulings made while planning (2026-10-21)

The implementation plan (`docs/design/plans/2026-10-21-tq-hub-core.md`) resolved these points where the sections above conflicted or were silent. The plan's reading is binding; the sections above are not rewritten.

1. **Decision ids (§6).** Decisions mirrored from the gate keep the gate's own id (`d` + 8 hex). Only hub-owned decisions (`permission`, over-budget) use `d-` + 12 base32.
2. **When the review page is built (§8.4, §10).** `finished` only becomes true after the human approves the `result` record. The card moves to `review` and the review page is built when the run is waiting on a pending `result`; a run finished from the terminal is handled the same way.
3. **Notifications (§8.7).** There is no existing shell helper in tq; a small `internal/hub/notify` package sends one bounded (2 s) best-effort notification per platform.
4. **`--all` opening (§5).** Workspaces that already have a `hub.db` are opened at start so their queues run; the rest open lazily on first request. Untrusted workspaces are never opened.
5. **Agent-session detection (§15).** `CLAUDE_PLUGIN_ROOT` is not set inside the Bash tool, so `CLAUDECODE=1` is a third signal. Consequence: `tq hub answer` typed through `!` inside a Claude Code session is refused; the human answers from a terminal or the page.
6. **Relocate resume form (§13.4).** No plan exists yet when the hand-off happens, so after the relocate the hub reruns the on-ramp prompt in the worktree; `/tentaqles:build resume <slug> --headless` is used only once a plan exists.
7. **Approach section source (§10).** The gate signs `approved.json`, not the plan summary. The plugin gains `build-gate state --records`, which marks each plan checkpoint as still covered by the approval or not; the review page reads that.
8. **One live hub per workspace (§17).** A second hub for a workspace already served is refused (or listed unavailable in `--all`); two hubs on one queue would fail each other's runs on start.

Also recorded: the `runs` table carries queue timestamps, phase, retry count, relocations, allowed tools, round log, seconds, kind and `updated_at`; `decisions` carries a `data` column. Test hooks `TQ_HUB_CLAUDE_CMD`, `TQ_HUB_GATE_CMD`, `TQ_HUB_PLUGIN_ROOT`, `TQ_BASH` and the hidden `tq hub serve --drain` exist for tests only. `hub-port-busy` is reported only when some workspace has a hub database.
