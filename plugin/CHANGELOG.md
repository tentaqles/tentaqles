# Changelog

All notable changes to the Tentaqles plugin. Versions follow [semver](https://semver.org/).

## [Unreleased]

## [0.14.0] — 2026-10-28

Board sync (tq spec B). Needs a `tq` build with `tq hub sync`, `tq secret` and `tq onboard`.

- **`/tentaqles:onboard`.** Connects and verifies the company tools of a client workspace
  from its manifest: prints the logins for the user (`tq login <ws> <id>`,
  `tq secret set <ws> <provider>`, `acli jira auth login`), guides `/mcp` for chat and email
  connectors, verifies with `tq doctor --json` from the workspace folder and one read-only
  call per loaded connector, and ends with a connected table. It never handles tokens and
  never opens `.claude.json` or the bundle catalog.
- **`/tentaqles:sync`.** The board sync's MCP fallback: when tq has no background
  credential (`needs-session`), it reads assigned items with the tracker's connector into
  `tq hub sync import`, sends only the rows `tq hub sync pending` lists (already redacted)
  and records each with `tq hub sync ack`. It never answers a send/skip decision and never
  runs the tracker CLI.
- **`/tentaqles:plan-cards <release>`.** One `feature` card per FR of a PRD release
  (`1`, `Release 1`, `release-1`, `R1`, or `all`), titled `<FR-id> <title>`, with the FR
  text, the acceptance checks and a `Source:` line as the body, under `--parent` and
  `--project`. Card ids are written back into PRD §11 between `<!-- tq:cards -->` and
  `<!-- /tq:cards -->`; a re-run skips FRs that have one, and an interrupted run resumes.
  It never tags `agent-ready`. Backed by `scripts/plan-cards.py` (JSON output; exit 0 ok,
  1 a tq call failed, 2 usage or PRD error, 3 PRD not planning-ready).
- **`/tentaqles:refine <card>`.** Takes one planning-lane card to `agent-ready`: reads the
  card, its FR and the parent, runs grill-me, writes the refined body (FR text, rules
  decided, acceptance checks, open questions, source) to a temp file, posts it as a card
  comment and prints `tq hub edit <card> --body-file "<path>"` for the human (a body edit
  is human-only), then adds the `agent-ready` tag. It refuses headless sessions.
- `tentaqles/workflow/prd.py` parses PRD §4 FRs and §11 release membership, and records
  card ids (`parse_frs`, `parse_releases`, `release_frs`, `fr_cards`, `record_cards`,
  `card_body`); `prd_status` uses the new `text_status`.
- `/tentaqles:hub` has a Board sync section: linked cards, the human-only send/skip
  decisions, and `tq hub push` / `tq hub sync retry` as human-only.

## [0.13.3] — 2026-10-27

- Docs: `/tentaqles:hub` explains the hub's own `question` (asked when a run stops before its build exists: `answer` with a required note, or `stop`) and that a plain card comment reaches the next round but does not resume a waiting run; the plugin README rows for `babysit-pr`, `ci-pipeline`, `dod` and `hub` match 0.13.2.

## [0.13.2] — 2026-10-27

- `ratchet.py compare` prints `unmeasured: <names> - record CI's measurement with: ...` (stdout and step summary) when a baseline metric is null, and `/tentaqles:babysit-pr` step 1b downloads the `quality-reports` artifact, runs `compare --update` and commits `ci: record first ratchet baseline from CI` to the PR branch, so an unmeasured baseline binds without anyone remembering `--update`.
- Fixed: a new project's first PR no longer fails CI on placeholders. `ratchet.py` accepts `"value": null` in a baseline as "unmeasured": `compare` passes and reports it, `compare --update` records the first real measurement, then the metric ratchets as usual (`init --force` remains the only overwrite). `/tentaqles:kickoff` and `ci-pipeline` write `coverage_pct` and `duplication_pct` as `null` instead of `0`. The CI templates gain a `secrets` (gitleaks) job with job-level `pull-requests: read`, which gitleaks-action needs on `pull_request`.
- Definition of done gains a `prd-decisions` check: it fails on a `NEEDS-DECISION` line in `docs/prd.md` section 10 that is open and was not open at the build's base (no recorded base counts every open one as new); pre-existing ones are listed, not failed. The build skill's final review lists all open decisions.
- Headless stop text points at the hub: `build`, `feature`, `kickoff`, `fix`, `dod` and `direction` never tell the human to run a terminal command (`build-gate approve`/`answer`, `/tentaqles:build resume … --headless`, re-running the skill) in headless output; with `TQ_HUB_TASK` set they say the answer goes on the hub card (`/waiting`) and the hub resumes the run, and they read the prompt's "Notes from the human on this card" as the human's answers. Interactive wording is unchanged. The build gate's Stop message for an unproven definition of done follows the same rule under `TENTAQLES_HEADLESS=1`: it says to record an `override` question for the hub card instead of naming `dod --override`.

## [0.13.1] — 2026-10-27

- Docs: `cloud.also` (extra clouds a workspace may use) in the plugin README and `docs/`; accepted names are `azure`, `aws`, `gcp`, `digitalocean` plus the aliases `az`, `gcloud`, `do`, `doctl`.
- `/tentaqles:client-settings` can add and remove `cloud.also` entries (a re-trust with `tq allow` follows).
- Docs: `tq login <ws> aws` uses AWS SSO when the private config has an sso-session, and `expected_account` for aws (account id).

## [0.13.0] — 2026-10-26

- Fixed: skill bash blocks and `tq_env.sh` picked the first plugin version in the cache alphabetically (0.10.0 over 0.12.0); they now pick the highest installed version, the identity cache winning ties.

### Added

- **`/tentaqles:hub`.** Lists the decisions hub runs are waiting on (`tq hub list
  --waiting --json`, bounded to 2 s), explains them, adds and shows cards, and prints the
  exact `tq hub answer` line for the user; it never answers itself.
- **Hub task block.** When `TQ_HUB_TASK` is set, `feature`, `fix` and `kickoff` pass it to
  the planner, which writes it verbatim as the plan's top-level `task`
  (`{"hub","workspace","kind","external"}`). The gate validates the shape, refuses a `task`
  the environment did not set or changed, snapshots it at approval and reports the
  approved one in `state --json`.
- **`build-gate state <slug> --json --records`.** Adds the gate's own signed records (plan
  summary and checkpoints with their approval status, verify evidence, reviewer verdicts,
  DoD) for the hub's review page.
- **Headless worktree hand-off.** A headless build ends Phase 0 with the last line
  `TQ_HUB_RELOCATE <absolute worktree path> <slug>`, once per slug; the hub starts the next
  round in the worktree.
- **`ship` posts the PR link** to the hub card (`tq hub comment -`, bounded, failure
  ignored) when `TQ_HUB_TASK` is set.
- **Session preamble** prints "N decisions waiting on you (tq hub open)" from a read-only
  SQLite read bounded to 300 ms.

- **Headless runs never edit `.tentaqles.yaml`.** In a hub run (`TENTAQLES_HEADLESS=1`),
  `classify-data`, `kickoff`, `client-settings` and `rollback` write the proposed change to
  `docs/privacy-manifest-proposal.yaml` and raise a decision (`build-gate ask <slug> --kind
  question --options applied,stop`) or a `NEEDS-DECISION` line; the user applies it and
  re-trusts. A new PreToolUse hook, `headless-manifest-guard.py` (5 s timeout, matcher
  `Edit|Write|MultiEdit|NotebookEdit|Bash|PowerShell`), blocks headless writes of the
  manifest: file tools (Windows spellings, streams, short names) and shell commands parsed
  line by line, with heredoc bodies read as data unless they feed a shell or an
  interpreter. Reads and mentions are allowed; outside headless runs it always allows.

### Changed

- A plan's `task` block in the old `{"hub_id", "external"}` shape is refused; the hub
  writes the new shape.

## [0.12.0] — 2026-10-21

### Added

- **Security and privacy pack.** Skills `threat-model`, `classify-data`,
  `privacy-review`, `supply-chain` and `llm-security-review`; read-only agents
  `security-reviewer`, `privacy-reviewer` and `llm-security-reviewer`
  (tools Read, Glob, Grep) and the `breaker` agent (below). Templates under
  `templates/process/` for the threat model, the privacy record and the
  incident playbook. Checklists with stable row ids sit next to the skills
  (`skills/<name>/references/`): STRIDE prompts, a data dictionary, and
  `checklist.md` for privacy (LGPD/GDPR), supply chain (SLSA/SBOM) and LLM
  security (OWASP LLM 2025 and Agentic 2026). The build crew adds
  `security-reviewer` on the auth/secrets/api/state/destructive flags or a
  workflow or dependency-manifest change, `privacy-reviewer` on the
  `classified_data` flag or a diff in `classified_paths`, and
  `llm-security-reviewer` on the `agentic` flag; `build-gate record-review`
  accepts all three. `/tentaqles:classify-data` writes the manifest `data:`
  block (classes, regions, paths and tables per class, retention, incident
  authority), `docs/privacy.md` and `docs/incident-playbook.md`. Only the user
  accepts a risk in the threat model; an unmitigated high-risk row becomes a
  requirement or a `NEEDS-DECISION` item. The skills have a headless mode and
  do not commit on `main`/`master`. Needs a tq that knows the manifest `data:`
  block, `tq redact`, the `classified_data` and `agentic` triage flags and the
  guard rules `tq/commit-pii`, `tq/push-pii`, `tq/text-pii`,
  `tq/pii-fixture-dump`, `tq/iac-apply`, `tq/remote-migration`,
  `tq/settings-widen`, `tq/settings-shell-write`, `tq/gha-branch-ref` and
  `tq/commit-message`.
- **`breaker`, the adversarial QA agent.** Runs only for checkpoints marked
  `"runnable": true`, starts the app with the project's `commands.run` on
  127.0.0.1, and returns BLOCKED when a data store host is not provably local.
  It backs up and restores local test data (stop, restore, verify on every
  exit path; `RESTORED: no` forces BLOCKED), blanks third-party keys and stops
  the app's whole process tree. Its scratch directory is
  `<root>/.tq-breaker/<slug>/`: data files only (the gate refuses code, test
  and config file names there), git-excluded by `build-gate setup`, left out
  of the evidence hash and deleted by `build-gate finish`.
- **Data classes mask what leaves the machine.** `tentaqles.privacy` gains the
  classes `pii`, `sensitive`, `financial`, `children`, `secrets`, `public`,
  `internal`, `checksummed` and `all`, with CPF, CNPJ, card (Luhn), IBAN,
  e-mail and phone detectors. Read paths (`classes=None`) mask secrets only,
  as before. Exits mask with the workspace's classes: every `memory.db` write,
  the Obsidian export, the markdown ingest (masked before it is truncated),
  signals, Jev payloads, model prompts, loop ideas and skill-correction
  records. An unknown class or an empty class list fails closed to the
  strictest set. The manifest `data.prompts.allow` governs model calls only;
  memory and the vault always mask. `tq:allow-pii` marks a deliberate line.
- Gate: shell commands that create links (`ln`, `mklink`, `fsutil`, `tar`,
  `unzip` and similar) are refused when they touch the gate's state paths or
  `.tq-breaker/`; output options such as `-o`, `--output` and `--file` count
  as writes to the path they name; `build-gate verify` and the definition of
  done walk `.tq-breaker/` (DoD check `breaker_scratch`); `approve` and
  `finish` validate the slug, so `finish ..` cannot delete anything outside
  the scratch directory.
- Manifest `data.classes` also accepts the two detector-group names `checksummed`
  (CPF, CNPJ, card, IBAN) and `all` (every detector, the strictest set), as tq's `internal/redact.Known` does.
- **Development process spine.** New skills `prd`, `kickoff`, `feature`, `fix`,
  `direction` and `dod`; agents `spec-critic` and `arbiter`; process templates
  under `templates/process/` (PRD, ADR, spec slice, fix spec, LEARNINGS,
  project CLAUDE.md scaffold, review contract).
  - `/tentaqles:kickoff` is strict: it needs a planning-ready PRD (it runs
    `prd` otherwise). `/tentaqles:feature` continues on a PRD that is not
    planning-ready, adds its own `### FR-nn` with binary checks, and settles
    only the `NEEDS-DECISION` items that touch it. `/tentaqles:fix` is
    reproduce-first and locks the regression test before any fix.
  - The review crew is `build-critic` always, plus `spec-critic` for
    `feature`/`kickoff` plans (and for `fix` plans that touch files outside
    the failing path) and `breaker` only for checkpoints marked
    `"runnable": true`. `/tentaqles:fix --hotfix` (user-passed only, never
    inferred) skips the critic; the locked regression test is never skipped.
  - The skills never commit on `main`/`master`: they open or switch to a
    `feat/<slug>` branch (or the build worktree) first.
  - **Review crew with recorded verdicts.** `build-gate record-review` stores
    each reviewer verdict against the tree hash; a review pass is refused
    without a `build-critic` PASS at the current tree. A BLOCKED verdict is
    settled by a human with `build-gate judge` (human-only).
  - **Definition of done.** `build-gate dod` runs the project commands frozen
    at approval and checks locked tests, checkpoint and reviewer records and
    the obligations the diff created (ADR, privacy record, migration plan,
    changelog). The Stop hook requires a fresh DoD for `feature`, `fix` and
    `kickoff` plans: it blocks a stop once, only when every frozen checkpoint
    has passed, the run is not finished and the DoD is not fresh.
    `build-gate dod --override "<reason>"` is human-only: the agent never
    runs it and the hook refuses it. A human override is kept while the code
    is unchanged and the approval id is the same (logged as
    `dod-override-kept`) and dropped on any code change; overrides are logged to `docs/LEARNINGS.md` and counted
    by `tq insights`.
  - **Hub-ready interfaces.** Decision records (`build-gate ask`, run by the
    agent; `build-gate answer`, human-only like `judge`, `token` and
    `dod --override`; stored in `decisions.jsonl`). Answering `override` on
    an `override:` question runs the DoD override, and `approve` on a
    `result` decision runs finish. Hub actor tokens (`build-gate token`, HMAC
    with the gate key; the hub passes `TQ_BUILD_ACTOR`/`TQ_BUILD_TOKEN`) and
    `build-gate state <slug> --json` (includes `dod_fresh` and answered
    decisions). Headless mode (`TENTAQLES_HEADLESS=1` or `--headless`) never
    calls `AskUserQuestion`: each human stop becomes a decision record and the
    turn ends.
  - Whenever a build has a plan or a state dir, the gate hook refuses the
    known routes around the CLI: shell commands that import or reference the
    gate module, the same names in the content of files the agent writes,
    writes under the gate's code, the known spellings of key paths (split strings,
    globs, `$TQ_HOME`/`~`/`%TQ_HOME%`, a recursive read of a parent directory,
    and Read/Grep/Glob calls aimed at the key directory or an ancestor of it),
    and git commands that touch `.claude/build/`. These are text
    checks, not isolation: the key is still readable by the agent's OS user
    until tq isolates it (OS-level key isolation is a tq follow-up), so the gate guards against a careless
    agent, not one set on defeating it. The signed records are what the human
    and the hub trust. Rollback of the signed state is detected by a
    generation record kept with the key, and unknown copy utilities remain a
    documented gap until OS-level key isolation lands.
    Every signed state file and record (`session.json`, `dod.json`, each
    `reviews.jsonl` and `decisions.jsonl` line) is bound to its project, slug
    and approval generation, so one copied from another build or an older
    generation is ignored, stale or reported as a rollback.
  - The approved checkpoint ids are the source of truth: a plan edit that
    drops or renumbers one is refused unless a plan approval is pending, and
    the DoD, the Stop hook and `state` count a removed checkpoint as not
    passed. `approve` shows the frozen DoD commands and refuses a trivial or
    non-string one; a hotfix plan is written with review `pending` and the
    approval switches it to `n/a`.
  - The tree hash works after `build-gate setup` on git 2.55 (an exclude
    pathspec naming the ignored state dir made `git add` exit 1).
  - `tq insights` gains a Process section from
    `$TQ_HOME/build-gate/events.jsonl`. SessionStart points at the PRD and
    counts open `NEEDS-DECISION` items. `add-project` scaffolds the fixed
    `CLAUDE.md` sections; `ship` quotes the DoD record (check id, status and
    the first detail line only, reading `dod_fresh` from `build-gate state`
    and re-running the DoD only when stale) and flags diffs over ~400 lines.

### Changed

- `build-critic` returns the answer-key grammar (`RESULT`, `BUILT-BY-ME`,
  `F`/`U` rows) instead of `VERDICT`. Plans gain top-level `workflow`,
  `hotfix` and `spec` fields; `review: n/a` is allowed only for hotfix plans.
  `build-planner` documents a per-checkpoint `"runnable": true` field, set
  when the checkpoint produces something the project's `commands.run` can
  start, so the build crew can add the `breaker` reviewer.
- `ci-pipeline` names the rule ids: `tq/gha-branch-ref` denies `@main` and
  `@master` in workflow files and `tq/gha-unpinned` asks on `@latest`; it
  points to `/tentaqles:supply-chain` for the full checklist.
- `tq allow`, `build-gate judge`, `approve`, `answer` and `dod --override` are
  human-only everywhere; no skill tells the agent to run them.

### Fixed

- **Flaky `test_spawn_detached_returns_while_the_child_is_still_working`.** It
  read the marker the moment it appeared, before the detached child wrote the
  payload; it now polls, bounded, until the content is complete.

## [0.11.1] — 2026-10-20

### Added

- **Memory search works without fastembed and prefers recent rows.**
  `MemoryStore.search_memory` now uses SQLite FTS5 (BM25) over ended
  sessions, active decisions, open pending items and semantic facts, with the
  score multiplied by a recency factor (30-day half-life, never below half)
  so the newer of two equal matches wins. Before, search was embeddings-only:
  it returned nothing when fastembed was missing, covered only sessions and
  decisions, and ignored age. The index is a derived table rebuilt when its
  row count disagrees with the base tables, so rows written by other hook
  processes are found on the next search. Without FTS5 it falls back to word
  matching. `query-memory` and `decision-history` use it.
- **Fix atoms.** A Bash command that fails is remembered (latest failure per
  program, one hour); the next successful command running the same program
  becomes a `fix` atom: "<error> -- fixed by: <command>". Atoms are indexed by
  the search above, and the newest three appear at session start under
  "Recent fixes". Error text is redacted and capped at 400 characters.
- **Obsidian mirror.** Each workspace's memory is written as Markdown notes
  under `<vault_dir>/<client>/`: `context.md` (client, stack, providers, hot
  files, last session), `decisions.md` (active decisions, confirmed),
  `plan.md` (open items as tasks, proposed), `fixes.md`, and one note per
  ended session under `sessions/` (written once, never rewritten). Every note
  has frontmatter (`type`, `client`, `status`, `updated`) for Obsidian Bases.
  The SessionEnd worker exports after saving; `/tentaqles:export-vault`
  refreshes on demand. Configure `vault_dir` in `~/.tentaqles/knowledge.yaml`
  (shared), `<plugin data>/knowledge.yaml` (per identity), a `knowledge:`
  section in `.tentaqles.yaml`, or `TENTAQLES_VAULT_DIR`; off until set.
  A manifest's paths are confined (its `vault_dir` must lie inside the
  user-configured vault, its `ingest_dirs` inside the workspace), since the
  manifest travels with the repository; the ingest never follows symlinks out
  of a folder.
- **Markdown folders as a connector.** `ingest: {<client>: [<folders>]}` in
  `knowledge.yaml` (or `knowledge.ingest_dirs` in the manifest) indexes every
  `*.md` under those folders into that client's memory search as `reference`
  items, keyed by path and mtime so unchanged files cost one stat. The note's
  own `published`/`ingested_date` is its date for recency weighting. Runs in
  the SessionEnd worker and in the skill.
- **Semantic facts carry the question that finds them.** The consolidator
  asks the model for each fact plus the question a teammate would type to
  find it (`fact || question`), stores it as `query_hint`, and search indexes
  both.

### Fixed

- **PostToolUse capture read the wrong field.** Claude Code hands hooks the
  tool result as `tool_response`; `knowledge-capture.py` read `tool_output`,
  so decision-pattern scanning over command output never fired on a real
  session. It now reads `tool_response` (dict or string) and still accepts
  `tool_output`.

- **Skills find the plugin under a tq identity.** Every skill bash block
  looked for the plugin only in `~/.claude/plugins/cache`, but when tq
  manages the session the cache lives in the identity's own config dir
  (`$CLAUDE_CONFIG_DIR/plugins/cache`). The search found nothing, sourcing
  `tq_env.sh` failed silently and the Python that followed died with
  `ModuleNotFoundError: No module named 'tentaqles'` (seen in
  `/tentaqles:session-wrap` and other skills). All 27 blocks and the
  `tq_env.sh` fallback now search `$CLAUDE_CONFIG_DIR` first, then
  `~/.claude`; `tests/test_skill_bootstrap_snippet.py` runs the snippet under
  bash and pins the shape of every block.

## [0.11.0] — 2026-10-09

### Added

- **`guard` skill.** Lists tq's guard rules and turns them off or on, or
  changes ask/deny, for one workspace or for all, through `tq guard`
  (tq >= 0.7.0). Every change asks you to confirm; the skill never edits
  guard files by hand.

## [0.10.0] — 2026-10-09

Phase 5–7 of the workflow upgrade. Every Jev feature starts in shadow mode
(it logs, never acts) and needs tq >= 0.6.0.

### Added

- **Hooks:** `Stop` runs `tq claude-hook stop` (completion-evidence check:
  catches "all tests pass" with no successful test run after the last
  edit; evidence comes only from tool records) and `UserPromptSubmit` runs
  `tq claude-hook prompt-submit` (skill picker, log-only). Both have short
  timeouts and always exit 0 when tq is missing or older.
- **Skills:** `ship`, `babysit-pr`, `wrap`, `n8n-triage`, `db-migration`,
  `index-advisor`, `ci-pipeline` (ratchet gate), `auth-security`,
  `system-design-review`, `grill-me`, `explore` (`tq decide explore`),
  `build` (gated spec -> plan -> tests -> implement -> review orchestrator)
  and `loop` (overnight improvement runner). Agents: `build-planner`,
  `build-critic` (read-only tools), `build-fixer`.
- **Shadow Jev memory gates:** capture and recall decisions are judged by
  Jev in a detached worker and logged; behaviour is unchanged. Opt out with
  `TENTAQLES_JEV_MEMORY=0`.

### Fixed

- **`knowledge-capture` recorded nothing.** It opened `MemoryStore` with
  the database file path instead of the workspace root, so PostToolUse
  file touches were never saved. They are now.
- **Private keys are redacted as whole blocks** (PEM, PGP, SSH2, PuTTY),
  not just the header line, before anything is stored.
- `emit-signal`'s frontmatter is valid YAML.

### Notes

- `/tentaqles:build`'s gate checks are string heuristics like tq's guard,
  not an OS sandbox: treat a passed gate as advisory and keep CI + PR
  review as the proof. `/tentaqles:loop`'s runner re-verifies every round
  itself (score, tests, changed paths, pinned hashes) and discards
  mismatches.

## [0.9.0] — 2026-10-09

### Added

- **Subagent launches go through tq.** The PreToolUse matcher now includes
  `Agent`, so `tq claude-hook pre-tool-use` (tq >= 0.5.0) can route a
  general-purpose subagent to a cheaper model when the workspace's
  `decision` policy enforces it and Jev is confident. In shadow mode (the
  default) it only logs the pick. Older tq versions allow `Agent` calls
  unchanged.

## [0.8.2] — 2026-10-09

### Fixed

- **A Python change no longer silently breaks memory features.** The
  bootstrapped lib dir holds compiled packages (numpy) built for one Python;
  under another they fail to import, but the dependency check only imported
  pure-Python packages and its stamp ignored the interpreter. The stamp now
  includes `TENTAQLES_PY`, the check imports numpy, and `bootstrap.py`
  passes `--upgrade` so a mismatched lib is replaced instead of skipped.

## [0.8.1] — 2026-10-08

### Fixed

- **Hooks no longer stall when the Microsoft Store Python breaks.** The
  interpreter resolver probed `py -3` first with no time limit; when the
  launcher picked a broken Store Python it hung, and every hook waited on it.
  Probes now run `python3`, `python`, then `py`, each bounded to 5 s, and any
  `WindowsApps` path (Store Python or its alias) is skipped, including one
  already in the interpreter cache.
- **`snapshot-guard` and `knowledge-capture` have timeouts (10 s).** They had
  none, so a stuck run held the tool call for Claude Code's 10-minute default.
  `knowledge-capture` also runs async: it records, it never blocks.

## [0.8.0] — 2026-10-08

Hook diet and reliability: fewer, cheaper hook interruptions, and memory
features that actually run.

### Fixed

- **The Stop hook's "record decisions" command works.** Its instruction told
  the model to run `bash "$CLAUDE_PLUGIN_ROOT/scripts/tq_run.sh" memory-bridge.py`,
  but `CLAUDE_PLUGIN_ROOT` is only expanded in hooks.json — it is not set in
  the Bash tool — so the command failed with exit 127. The instruction now
  embeds the plugin's absolute path (forward slashes) and a properly quoted
  JSON payload.
- **Compaction context reaches the model.** PreCompact hook output is never
  shown to the model, so the state block `pre-compact.py` printed was dropped.
  `session-preamble.py` now prints it (recent decisions, hot nodes, open
  pending) when SessionStart fires with `source: "compact"`. The `PreCompact`
  registration is removed (it saved no state); `pre-compact.py` remains as a
  manual/debug entry point. The block is only built when the workspace's
  `memory.db` already exists, so reading it never creates one.
- **First-run dependency install no longer blocks session start.** The
  SessionStart `bootstrap.py` hook used to run `pip install` (fastembed
  included) inline with a 600s timeout. It now starts a detached
  `bootstrap.py --worker` (via `_detach.py`), prints one notice line and
  returns; a `.bootstrap.lock` in the data dir (stale after 20 minutes) stops
  concurrent sessions from installing twice, and the worker logs to
  `bootstrap.log`. The hook timeout drops from 600s to 30s. `tq_env.sh`
  discards the notice when it runs bootstrap itself so it cannot corrupt
  another hook's JSON output.

### Changed

- **The decision prompt fires far less often.** Besides once per session
  (markers now live in `$CLAUDE_PLUGIN_DATA/stop-capture/<session>.done`,
  pruned after 30 days), never when `stop_hook_active`, and at least 12 tool
  calls, it now also requires the conversation itself — user prompts and
  assistant prose, never tool output — to show a choice between alternatives
  ("instead of", "rather than", "trade-off", "decided", "vs", "option A",
  "em vez de", "decidimos", "optei", ...). Sessions that only did work are no
  longer interrupted.
- **Session-size nudge.** When the last assistant turn's context
  (`input_tokens + cache_read_input_tokens + cache_creation_input_tokens`)
  exceeds 350k tokens, the Stop hook shows a one-time, non-blocking
  `systemMessage` suggesting `/compact` or the session-wrap skill. Override
  the threshold with `TENTAQLES_CONTEXT_NUDGE_TOKENS` (`0` disables).
- The SessionStart `session-preamble.py` hook now declares a 30s timeout.

### Added

- **Semantic facts are extracted.** The fact-extraction step in memory
  consolidation never ran because no model was passed. The detached
  session-end worker and `compaction-cron.py` now use
  `tentaqles.memory.llm.get_semantic_llm()`, which runs
  `claude -p --model haiku --output-format json` (prompt on stdin, redacted
  with `privacy.redact_text`, 60s timeout) under the current environment, so
  `CLAUDE_CONFIG_DIR` — the active identity — is respected. Missing `claude`,
  errors and timeouts yield no facts rather than failures. Never runs in a
  foreground hook. Opt out with `TENTAQLES_SEMANTIC_FACTS=0`. The headless
  child runs with `TENTAQLES_HEADLESS_CHILD=1`, which the plugin's own
  SessionEnd and Stop hooks honour by doing nothing.

## [0.7.0] — 2026-10-08

### Changed

- **The `PreToolUse` hook now covers PowerShell, file tools and MCP calls,
  not just Bash.** The matcher is
  `^(Bash|PowerShell|Read|Edit|Write|MultiEdit|NotebookEdit|Grep)$|^mcp__`.
  `tq claude-hook pre-tool-use` runs the identity guard on PowerShell
  commands too (a `git push` through the PowerShell tool used to skip it)
  and applies tq's policy rules to every call: deny reading SSH keys and
  credential stores, ask before reading `.env`, deny `printenv` dumps and
  force-pushes to main, ask before cloud deletes, `--admin` merges, n8n
  publishes, database writes through MCP, and edits to existing
  migrations, and refuse a `git commit` that would record a secret. See
  `docs/CLAUDE-HOOK.md` → "Policy rules". Requires `tq` 0.2.0 or
  later; with an older `tq` the extra tools are simply allowed.
- **Fallback with neither `tq` nor Python only blocks shell tools.**
  `tq_hook.sh` still fails closed for Bash/PowerShell, but lets Read,
  Edit and MCP payloads through instead of blocking every file read.

## [0.6.0] — 2026-10-01

Session saving at exit was failing far more often than it appeared to, in
several independent ways. This release fixes them and adds a tool to recover
what was already lost.

### Added

- **`scripts/repair-sessions.py` backfills half-written session rows.** Sessions
  whose `SessionEnd` hook was killed part-way left rows with a start time but no
  end and no summary. The script re-reads the transcript each row names, through
  the same parser the hook uses, and fills them in. It is a dry run by default;
  `--apply` writes, after copying each database to `.pre-repair`. `--prune`
  deletes empty rows nothing else references. Run it with
  `bash "$CLAUDE_PLUGIN_ROOT/scripts/tq_run.sh" repair-sessions.py --all`.

### Changed

- **`fastembed` is an optional dependency.** It moved from the required
  dependencies to an `embeddings` extra, so `pip install tentaqles` no longer
  pulls onnxruntime and model downloads. Memory writes without it store a NULL
  embedding and otherwise succeed. Nothing changes for plugin installs:
  `bootstrap.py` still installs it, so semantic search works out of the box.

### Fixed

- **`SessionEnd` no longer loses the session to Claude Code's exit budget.**
  Claude Code aborts every SessionEnd hook still running after 1500ms and
  reports it as `Hook cancelled`; the `timeout` a plugin declares in
  `hooks.json` does not raise that. The save takes about that long on its own
  for a large transcript, before counting the launcher, so sessions were being
  dropped or left started-but-never-ended.

  `session-end.py` now hands its payload to a detached copy of itself
  (`--worker`) and returns. The hook is back in roughly 0.6s; the save lands a
  second or two later, after Claude Code has gone. If the worker cannot be
  started the hook saves inline, as before.

  One consequence: after `/clear`, the next session's preamble can run before
  the worker has finished, so "last session" may lag by one.

- **The hook launcher no longer re-discovers Python on every hook.**
  `tq_env.sh` probed for an interpreter and re-checked dependencies on each
  invocation, two interpreter startups before the hook script began. Both
  answers are now cached in the plugin data directory and revalidated cheaply,
  so a warm run spawns nothing. The cache is invalidated by a plugin upgrade, a
  missing interpreter, or a deleted `lib` directory.

- **Session summaries reflect the work that was done.** Two transcript readers
  assumed a flat entry shape Claude Code never writes, so they matched nothing:
  every automatic summary read "Session with no file changes", no file touches
  were recorded at session end, and no open threads were detected. Parsing now
  lives in `tentaqles/transcript.py` and reads tool calls and user text from
  where they actually are. Tool output and model reasoning are not treated as
  something the user said.

- **No memory row references a session that does not exist.** With no active
  session, decisions and pending items were stamped with the shared `untracked`
  session id without that row being created. Every writer now goes through one
  helper that creates the placeholder first.

## [0.5.0] — 2026-08-31

### Added

- **Decisions are captured without being asked for.** Sessions and pending items
  were already recorded automatically at session end; decisions were written
  only when the `session-wrap` skill ran, which needs somebody to remember to
  say "done" before closing the terminal. In practice that produced nineteen
  days in which every session was logged and not one reason for anything was —
  the what recorded, the why lost.

  A new `Stop` hook asks the model to record what was decided, once per
  session, while it still holds the context. It fires only after a session has
  done real work, writes a marker so a conversation is never asked twice, and
  respects `stop_hook_active` — ignoring that flag is how a stop hook becomes an
  infinite loop.

- **A fallback for terminals that are simply closed.** `session-end.py` now also
  scans the transcript for decisions, the same way it already scans for open
  threads. It matches only text explicitly labelled as a decision, so it
  usually finds nothing. That is deliberate: an earlier version matched
  directives like "use X instead of Y" and, run against two real transcripts,
  stored mid-sentence fragments as decisions. Writing that into a store people
  rely on is worse than writing nothing. Anything it does capture is marked
  `confidence: low` and tagged `auto`.

## [0.4.0] — 2026-08-28

tq is the source of truth: hooks call `tq claude-hook`; Python guard is
fallback-only; preamble no longer switches gh/az/git; client-registry.json
removed; requires tq ≥ 0.1.0.

- `blocked_commands` entries now match as a whole-word prefix (or phrase) at
  the start of a command/segment; the old substring match is gone — an entry
  like `gh ` still matches `gh pr merge`, but `git` no longer matches
  `github`.

## [0.3.1] — 2026-04-16 — "Portable Runtime"

Full cross-platform compatibility for macOS, Linux, and Windows. Fixes plugin breakage on machines where `python` doesn't exist (macOS), resolves to a broken venv shim, or points to a Windows Store stub.

### Added

- **`scripts/tq_env.sh`** — POSIX-compatible runtime bootstrap. Resolves `CLAUDE_PLUGIN_ROOT` (env var → `$BASH_SOURCE` → filesystem search), finds a working Python interpreter (`py -3` → `python3` → `python`, validated via `sys.executable`), exports `PYTHONPATH`, and runs `bootstrap.py` if core deps are missing. Works in bash, zsh, and dash.
- **`scripts/tq_run.sh`** — thin wrapper: sources `tq_env.sh`, then exec's the target script with the resolved interpreter. Used by all hooks and skills.
- **`.gitattributes`** — forces LF line endings on `*.sh`, `*.py`, `*.json`, `*.md` to prevent CRLF "bad interpreter" errors when cloning from Windows to Unix.
- **Touch event acknowledgment.** The `touch` handler in `memory-bridge.py` now prints `{"touch_id": "..."}` on success, matching `decision` and `pending` handler behavior.

### Fixed

- **Duplicate hooks load error.** `plugin.json` no longer declares `"hooks": "./hooks/hooks.json"` — Claude Code auto-loads `hooks/hooks.json`, and the explicit reference caused `Failed to load hooks ... Duplicate hooks file detected`, disabling every hook. Version fields in `plugin.json`, `marketplace.json`, and `pyproject.toml` now all read 0.3.1.
- **macOS compatibility.** Hooks now use `bash tq_run.sh` instead of bare `python` (which doesn't exist on macOS Catalina+). The interpreter probe finds `python3` automatically.
- **POSIX shell compatibility.** Removed `${BASH_SOURCE[0]}` array subscript (breaks dash/sh on Ubuntu/Debian). Uses `$BASH_SOURCE` without subscript, guarded by `$BASH_VERSION` check, with glob fallback for non-bash shells.
- **Windows Store Python.** `bootstrap.py` detects the `WindowsApps/` stub executable and resolves the real interpreter via the `py` launcher before calling `pip install --target`.
- **Plugin cache discovery.** Skill preludes now check `.claude-plugin/plugin.json` (not `plugin.json` at root) and use a marketplace-agnostic glob (`*/tentaqles/*/` instead of `tentaqles/tentaqles/*/`).
- **`session_end` without prior `session_start`.** `MemoryStore.end_session()` auto-creates a session if none is active (lazy upsert).
- **Naive/aware datetime mixing.** `end_session` now treats legacy naive timestamps as UTC, preventing `TypeError` on databases migrated from pre-v0.3 versions.
- **XDG-compliant data paths.** Fallback plugin data dir uses `~/Library/Application Support/tentaqles` on macOS and `~/.local/share/tentaqles` on Linux (was `~/.tentaqles` everywhere).
- **Executable permissions.** `.sh` files are committed with `+x` bit so they work on macOS/Linux without manual `chmod`.
- All 17 skills updated: `"$TENTAQLES_PY"` instead of bare `python`, `PYTHONPATH` instead of `sys.path.insert` hacks.

### Breaking

None. All changes are backward compatible.

## [0.3.0] — 2026-04-13 — "Memory Matures"

Six architectural features that deepen how the plugin remembers, reasons about, and shares knowledge across workspaces.

### Added

- **4-tier memory consolidation with decay.** Brain-inspired tiers: Working → Episodic → Semantic → Procedural. Sessions auto-promote to Episodic on close; important facts climb to Semantic and Procedural over time. Ebbinghaus decay evicts stale entries automatically. New tables `semantic_memories` and `procedural_memories` in `memory.db`; new column `sessions.memory_tier`. Skill: `/tentaqles:compact-memory`. Cron: `scripts/compaction-cron.py`.
- **Contradiction detection and supersession.** When a new decision is recorded, it is embedded and compared cosine-similarity against active decisions. If similarity > 0.82 and the text disagrees, the old decision is auto-superseded — the chain is preserved, not deleted. New column `decisions.contradiction_score`. Skill: `/tentaqles:decision-history`. Programmatic: `MemoryStore.get_decision_lineage()`.
- **Time-travel snapshots.** Append-only JSON snapshots at `{workspace}/.claude/snapshots/{utc_iso}.json` capture the manifest, memory stats, and git identity at a point in time. Auto-fires on identity auto-switch and any Write to `.tentaqles.yaml` via the `snapshot-guard.py` PreToolUse hook. Keeps last 30; older pruned. Skill: `/tentaqles:rollback`.
- **Workspace profiles (learned, not declared).** Auto-generated profile from `memory.db` — hot files, session frequency, top concepts — written to `{workspace}/.claude/profile.json` and injected into the SessionStart preamble. Regenerates automatically when >7 days old. Skill: `/tentaqles:profile-refresh`.
- **Cross-workspace pattern detection.** Weekly background job reads decisions from all registered workspace memory.dbs (read-only), embeds them, clusters them, and surfaces patterns that span two or more workspaces. Output at `{data_dir}/metagraph/patterns.json`, surfaced in `MetaMemory.get_cross_workspace_context()`. Skill: `/tentaqles:cross-patterns`. Cron: `scripts/pattern-cron.py`.
- **Inter-workspace signals (pub/sub).** A `signals` table in the global `meta.db` lets Workspace A emit a message to Workspace B; B reads it on next session start. 48-hour TTL, acknowledge-once. Opt-in per workspace via `signals:` manifest block. Skill: `/tentaqles:emit-signal`.

### Schema

All additions backward compatible (`ALTER TABLE ADD COLUMN` with `OperationalError` catch). Existing v0.2 databases migrate on first open.

- `sessions.memory_tier` column (values: `working`, `episodic`)
- `decisions.contradiction_score` column
- New `semantic_memories` table
- New `procedural_memories` table
- New `signals` table in global `meta.db`

### Manifest

New optional section:

```yaml
signals:
  enabled: true
  subscribe_to: [dirtybird, acme-corp]
```

Missing section = feature disabled. Fully backward compatible.

### Build

- Tests: 60 → 238 (178 new)
- Pip dependencies added: 0
- New modules: 10
- New scripts: 4
- New skills: 6

## [0.2.0] — "Foundation"

The first public release after the rebrand. Six Tier 1 quick wins on top of the workspace identity / temporal memory / knowledge graph foundation.

### Added

- **PreCompact hook re-injection.** Critical state (manifest summary, decisions, hot nodes) is re-injected into the conversation before Claude Code auto-compacts, so important context survives compaction.
- **Privacy filter.** Every observation is scanned for secrets (API keys, JWTs, OAuth tokens, connection strings, private keys, cross-client emails) before touching disk. Integrated into store, session-end, knowledge-capture, and memory-bridge.
- **File history skill.** `/tentaqles:file-history` shows everything the plugin has recorded about a specific file — touches, decisions, pending items, across sessions.
- **Open thread auto-detection.** End-of-session parser detects unresolved threads from the transcript and records them as pending items automatically.
- **Self-improving skills.** User corrections are recorded to the skill's own definition at `{client_root}/.claude/skills/{name}/SKILL.md`. Per-client isolation — one client's corrections never affect another.
- **Real-time dashboard.** Runs at `http://localhost:8765` with a live grid of workspaces, hot nodes, and pending items. Pure stdlib, self-contained HTML, works offline.

Plus the v0.1 foundation: workspace auto-detection via `.tentaqles.yaml`, auto-switching git/gh/az/aws identity, preflight checks, per-client SQLite temporal memory, pluggable knowledge graph engine (graphify or native), and cross-workspace semantic search via the meta-graph.
