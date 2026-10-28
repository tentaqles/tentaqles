# Spec B: board sync, onboarding, PRD to cards, and the board GUI

**Parent:** `2026-10-21-tq-hub-core-design.md` (spec A), the second release-2 spec. **Source:** `docs/IDEATION.md`.
**Status:** draft for approval, 2026-10-27.

## 1. Goal

A developer joins a client. They get the whole flow, from one client folder:
- They set up the folder.
- They connect every company tool.
- Their own board mirrors the company tracker.
- A project card becomes a PRD, then child cards, then agent runs, then a review. The results flow back to the company board.

Everything is driven from a Claude Code session or from the board in the browser or the desktop app. Spec A already covers the runs, decisions, gates and the review page. This spec covers the rest.

## 2. Decisions (from the human, 2026-10-27)

| Topic | Decision |
|---|---|
| Trackers in release 1 | Azure DevOps Boards, Jira Cloud, Linear, Asana, GitHub (Issues and Projects v2). |
| Auth order | 1. The CLI's own login, where the tracker has a CLI (`gh`, `az`), or a token in the OS keychain. 2. A token through `tq dotenv` (the env var named in the manifest). 3. The agent's MCP connector, used from a Claude session (no background sync). |
| Outward writes | Defaults: these are automatic: lane moves when work starts, lane moves to review when you approve, and result comments. These ask first: creating a company card, editing title or description, closing an item. **Every one is a setting, with values `off`, `ask` or `auto`.** |
| GUI | Both. The browser board stays the main UI, and the desktop app gains a Board tab (a top-level webview on the hub) and native notifications. |

## 3. Non-goals

- Webhooks. They need a public URL, so release 1 polls with an updated-since watermark.
- ClickUp, Monday, Trello and Notion sync. The adapter interface allows them later.
- Editing company cards' attachments.
- Reading Slack, Teams or email from tq itself. Those are MCP connectors the agent uses, and onboarding only sets them up and checks them.

## 4. Architecture

```
tracker API/CLI ⇄ internal/hub/sync/<provider> (Adapter) ⇄ internal/hub/sync (Engine: poll, map, outbox) ⇄ store (cards, sync tables)
                                                                    ↑ settings: write policy, poll interval
Claude session ⇄ MCP connector ⇄ /tentaqles:sync skill ⇄ `tq hub sync import|outbox` (fallback 3)
```

- **One engine per workspace** runs beside `svc.Start` in `web.MultiBackend`, as a sibling goroutine. It also runs once on demand with `tq hub sync now`.
- **Inbound changes** pass through `Store.MoveCard` with the actor `sync`. A move that sync makes **never starts a run**: only a human move into an active lane starts one. Child rollup (`completeParent`) works unchanged.
- **Outbound writes** go through an outbox. A hub event queues an outbox row. The engine checks the write policy: `auto` sends it, `ask` creates a hub `permission` decision (`send` / `skip`), and `off` drops it. These are the events:
  - `card.move` of a linked card;
  - `review.approve`;
  - a run result comment;
  - a "push to company board" action;
  - `card.update` of a linked card's title or body by anyone but sync (queues `edit_text` with only the edited fields; a newer edit replaces a pending, not-yet-asked row). *(Added during the build, so the `edit_text` policy has an event.)*
  Every text that leaves the machine goes through `redact` with the workspace's `exit_classes` (the PII rule).

## 5. Data (store `schemaV2`, append-only)

- `cards`:
  - a UNIQUE index on (`external_provider`, `external_id`) where it is not null;
  - a new `external_rev` column (TEXT: the tracker's version, etag or updated-at), and `external_synced_at`.
- `sync_state(workspace, provider, scope, watermark, last_ok_at, last_error, last_error_at)`.
- `sync_outbox(id, card_id, action, payload JSON, status pending|sent|skipped|failed, decision_id, attempts, last_error, created_at, sent_at)`.
- `lane_map(provider, scope, external_status, lane)` is built from the manifest when sync starts. It is not user-edited in the database.
- New settings keys:
  - `sync.interval`: seconds, default 300, minimum 60;
  - `sync.policy.move_on_start`, `sync.policy.move_on_review`, `sync.policy.comment_results`: each `auto` by default;
  - `sync.policy.create_card`, `sync.policy.edit_text`, `sync.policy.close_item`: each `ask` by default;
  - `sync.enabled`: default true when a `tracker` block exists.

## 6. Manifest (names only, never secrets)

```yaml
tracker:
  provider: azure-devops        # github | azure-devops | jira | linear | asana
  scope:                        # what to mirror; provider-specific keys
    org: acme                   # ado org / jira site / github owner / asana workspace / linear team
    project: Platform
    board: Platform Team        # optional (ado team, github project number, asana project)
  assigned_to: me               # me | anyone; release 1 mirrors items assigned to me plus children of mirrored cards
  auth: cli                     # cli | keychain | env | mcp (fallback order is fixed: cli -> keychain -> env -> mcp)
  env: ACME_JIRA_TOKEN          # only for auth env: the variable name tq dotenv resolves
  lanes:                        # optional overrides; defaults map by status category
    "In Review": review
    "QA": review
chat:  { provider: slack }      # slack | teams | none  (MCP only)
email: { provider: gmail }      # gmail | outlook | none (MCP only)
data_platform: { provider: snowflake }   # any catalog data id, or none
agents: [claude, codex]         # which agent CLIs this workspace logs into
```

- Every block is optional (skippable).
- The loader validates provider names against the catalog.
- Unknown keys under `tracker.scope` are kept and passed to the adapter.
- A manifest change re-trusts the workspace, as today: `tq allow` is human-only.

## 7. Adapter interface (`internal/hub/sync`)

```go
type Item struct {
    ID, Key, URL, Title, Body, Status, StatusCategory string   // StatusCategory: backlog|todo|active|review|done
    Assignee, ParentID, Rev string
    Labels []string
    Acceptance string        // ADO AcceptanceCriteria, Jira custom field; else ""
    Comments []Comment       // newest first, capped 20
    Attachments []Attachment // name + url only, never downloaded
    UpdatedAt time.Time
}
type Adapter interface {
    Name() string
    Check(ctx context.Context) (account string, err error)               // verify auth; used by doctor and onboarding
    Statuses(ctx context.Context) ([]Status, error)                      // name + category, for lane mapping
    ListChanged(ctx context.Context, since time.Time) ([]Item, time.Time, error) // assigned/mirrored items changed since; new watermark
    Get(ctx context.Context, id string) (Item, error)
    Move(ctx context.Context, id, status string) error                  // jira: resolves the transition
    Comment(ctx context.Context, id, markdown string) error              // adapter converts (jira ADF, ado HTML)
    Create(ctx context.Context, in NewItem) (Item, error)                 // in.ParentID set -> child
    Update(ctx context.Context, id string, title, body *string) error
    SetLabels(ctx context.Context, id string, add, remove []string) error
}
```

- **Credentials** (`internal/hub/sync/cred`): `Resolve(ws, provider) (Transport, error)` tries each source in order.
  1. **cli:**
     - `gh api`, with `GH_CONFIG_DIR` from the identity env;
     - `az account get-access-token --resource 499b84ac-1321-427f-aa17-267ca6975798`, with `AZURE_CONFIG_DIR`;
     - Jira: `acli` if it is installed and logged in.
  2. **keychain:** the OS keychain, service `tentaqles`, account `<ws>/<provider>`.
  3. **env:** the manifest's `tracker.env`, resolved via `tq dotenv`.
  4. **mcp:** the engine reports `needs-session`, and the `/tentaqles:sync` skill does the work.

  A token is held in memory only for one request and is never logged. *(Amended during the build: a CLI-issued token such as `az account get-access-token` may stay in memory, keyed by the identity's config dir, until 5 minutes before it expires, and is dropped on a 401. It is never written to disk.)* Errors say which source failed, never the token.
- **Testing:** every adapter is tested against recorded HTTP fixtures (`httptest`) or a fake CLI on PATH. No test calls a real tracker.
- **Rate limits:** honour `Retry-After` and `X-RateLimit-*`, back off exponentially (a 3-attempt cap per tick), and record `last_error`.

## 8. Commands

- `tq secret set <ws> <provider>` reads the token from a hidden prompt or stdin. `tq secret rm`. `tq secret status`, which never prints values. **Human-only**: an agent session is refused.
- `tq hub sync now|status|outbox [--json]`.
- `tq hub sync import --json -`, for the MCP fallback. **Agents may use it**, but only into planning lanes, and only to create or update linked cards.
- `tq hub sync ack <outbox-id> --sent|--failed "<why>"`, also for the MCP fallback. Agents may use it only for outbox rows that are `auto`, or `ask` rows the human approved.
- `tq hub push <card>` pushes a local card to the company board, as a `create_card` outbox row under the policy (as a child of the parent's external item when there is one).
- `tq onboard [<name>]` is the interactive setup. It extends `tq add` and uses `huh`, like `tq setup`.
  - It asks for: identity email, display name, git provider, cloud (+ also), data platform, agents, tracker (provider + scope + auth), chat and email. Each question can be skipped.
  - It writes the manifest, prints the `tq allow` line for the human, and offers to open a new terminal in the folder (Windows Terminal `wt -d`, otherwise the desktop app's `OpenTerminal` logic). It then tells the user to close the current one. It never kills a terminal itself.
- `tq doctor` gains:
  - a `tracker` check (`Adapter.Check` plus the scope it resolves to);
  - `chat` and `email` checks (the MCP entry is present in the identity's `.claude.json`);
  - catalog fixes: the azure-devops login is `az login` and it gets a verify; jira gets `acli`; linear and asana get the env-var note.

## 9. Plugin skills

- **`/tentaqles:onboard`** runs inside the client folder. It reads the manifest and, for every provider in it, offers the login and verifies it:
  - CLIs: it prints the `tq login <ws> <id>` line, because a login is the human's to do, then runs `tq doctor`.
  - Tracker: cli, then the keychain (it prints `tq secret set`), then env.
  - MCP connectors (Slack, Teams, Gmail, Outlook, Linear, Asana, Atlassian, Notion): the `tq bundle` catalog entry, then `/mcp` auth guidance. It verifies with a read-only tool call when the tool is available.

  It ends with a table of what is connected. It never handles token values.
- **`/tentaqles:sync`** is the MCP fallback. It pulls assigned items with the connector, feeds `tq hub sync import`, works through the pending outbox rows the policy allows, and acks each one.
- **`/tentaqles:plan-cards`** turns a PRD into child cards.
  - It parses PRD §4 FRs (title, body, acceptance checks) and the release membership in §11 (`FR-1 to FR-5` ranges).
  - It creates one `feature` card per FR of the chosen release, with `--parent <project card>`, `--project <dir>`, and the body set to the FR text plus checks.
  - It writes the card ids back into §11.
  - It is idempotent: an FR that already has a card id is skipped.
  - It never tags `agent-ready`; that is the refine step.
- **`/tentaqles:refine <card>`** runs grill-me on one card. It writes the new card body (the answers and acceptance checks) to a file and hands the human a `tq hub edit <card> --body-file <file>` command, because title and body edits are human-only on the hub. It then adds the `agent-ready` tag itself, since agents may edit tags on planning lanes. *(Amended during the build: originally the skill updated the body directly.)*
- `/tentaqles:hub` learns about sync: linked cards, the outbox decisions, and `tq hub push`.

## 10. Hub UI

- **Connections page** (`/connections`, the placeholder in HUB.md). Per provider it shows:
  - the auth source in use;
  - the account;
  - last sync, last error, next run;
  - a **Sync now** button;
  - the lane map;
  - the six policy toggles (off/ask/auto).
- **Cards:**
  - an external badge and link (`ADO #123`);
  - the external lane;
  - a **Push to company board** button, for local cards;
  - outbox rows shown in the card's timeline.
- **Review approve** queues `move_on_review` (with `comment_results` holding the summary and PR link) for a linked card. For a child with no link, it comments on the parent's external item.
- **Desktop app:**
  - **Board tab:** it finds a running hub (`serveinfo.ReadAll` + `Ping`) or starts `tq hub serve` as a child process, then navigates a top-level webview to it. No iframe: the CSP `frame-ancestors 'none'` stays.
  - **Notifications:** the hub's notify setting stays the source.
  - The setup wizard adds a Services step for tracker, chat and email that uses the same manifest blocks.

## 11. Parallel safety

- Two cards of one project may run at once. A run's first round, before it relocates, now takes a **per-project pre-relocation lock** (`project_dir` key, in memory plus a store row). A second run waits until the first one has relocated into its worktree or ended. After relocation, each build has its own worktree.
- Sync and runs never write the same columns. Sync writes `external_*`, and title and body only for linked cards whose local copy is unchanged since the last sync. It moves lanes only through `MoveCard`. A lane conflict happens when both sides moved since the last sync. Then the tracker wins for inbound moves, a system comment records it, and an outbound move against a newer remote state is skipped with a comment.

## 12. Security

- Tokens live only in the OS keychain, the CLIs' own stores, or env via `tq dotenv`. They are never in files tq writes, logs, activity, comments or prompts.
- `tq secret` is human-only.
- Outbound text is redacted per `exit_classes`. Inbound text is masked before storage, as card bodies are today.
- MCP fallback: agents can import cards only into planning lanes and can only ack allowed outbox rows. They never answer `ask` decisions.
- A new guard rule asks before an agent runs a tracker CLI write (`az boards work-item update`, `gh issue edit` and similar) outside the sync engine.

## 13. Work breakdown (parallel)

| WS | Scope | Depends on |
|---|---|---|
| F | `schemaV2`, the `sync` package skeleton (Adapter, Item, Engine shell, outbox, policy settings), manifest blocks and validation, `TQ_HUB_TASK.external` filled | — |
| A1–A5 | Adapters: github, azure-devops, jira, linear, asana (fixtures, Check/Statuses/ListChanged/Get/Move/Comment/Create/Update/SetLabels) | F |
| K | Keychain and `tq secret`; the cred resolver chain | F (interface only) |
| E | The engine: poll loop in the MultiBackend, mapping, conflict rules, outbox to policy to decision, redaction, `tq hub sync`/`push`, approve hooks | F |
| O | `tq onboard`, catalog fixes and new MCP-only providers, doctor checks, the `/tentaqles:onboard` skill | F |
| P | `prd.py` FR and §11 parser, `/tentaqles:plan-cards`, `/tentaqles:refine` | — |
| U | Hub UI: Connections page, card badges, push button, policy toggles | F, E |
| D | Desktop Board tab, hub discovery and start, Services step | F |
| S | The per-project pre-relocation lock, the guard rule for tracker CLI writes, the `/tentaqles:sync` skill and `sync import`/`ack` | F, E |
| Doc | HUB.md, SETUP.md, PROVIDERS.md, DESKTOP.md, the plugin README, CHANGELOG, a release | all |

## 14. Done when

- With a fake tracker and recorded fixtures, every adapter passes: list, get, move, comment, create a child, update, labels.
- The engine imports, maps lanes, survives a 429, applies each policy value, and redacts outbound text.
- `tq onboard` writes a valid manifest with skipped blocks.
- `plan-cards` on the demo-todo PRD makes 6 cards with parents and is idempotent.
- The desktop Board tab opens the hub.
- `go test ./...`, the plugin suite and CI are green.
- An end-to-end check against a real tracker is a manual step for the human, with a real account. It is never run by an agent.
