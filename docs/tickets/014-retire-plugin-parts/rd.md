# 014: Cut the plugin down to onboarding, memory and the graph

**Status:** ready
**Risk:** High: this removes the plugin's guard hook, and if ticket 002's registration isn't in place an identity loses its guard; it also deletes code that can only come back from git history
**UI:** none

The owner's decision of 2026-11-06, narrowing PRD v2 §5.1: the plugin keeps the onboarding skill, session memory, the knowledge graph, their skills and hooks, and the session preamble. Everything else in it goes, including its guard hook, which the CLI now registers (ticket 002).

## Requirements

- **REQ-1**: `plugin/skills/` must hold exactly these 12 folders: `onboard`, `session-wrap`, `wrap`, `compact-memory`, `query-memory`, `decision-history`, `file-history`, `cross-patterns`, `emit-signal`, `export-vault`, `profile-refresh`, `build-graph`.
- **REQ-2**: These 28 skills must be gone: `ship`, `feature`, `fix`, `kickoff`, `dod`, `direction`, `prd`, `babysit-pr`, `grill-me`, `auth-security`, `db-migration`, `index-advisor`, `system-design-review`, `n8n-triage`, `threat-model`, `classify-data`, `privacy-review`, `llm-security-review`, `supply-chain`, `ci-pipeline`, `add-project`, `add-client`, `client-settings`, `switch-client`, `workspace-status`, `rollback`, `setup-demo`, `dashboard`.
- **REQ-3**: `plugin/agents/` must be gone, with its 5 reviewers.
- **REQ-4**: `plugin/hooks/hooks.json` must no longer hold a PreToolUse entry. SessionStart, PostToolUse, Stop and SessionEnd stay.
- **REQ-5**: `plugin/scripts/identity-guard.py` and `plugin/scripts/snapshot-guard.py` must be gone, and `plugin/scripts/tq_hook.sh` must only serve `session-start`, exiting 2 on any other event.
- **REQ-6**: These must be gone: `plugin/tentaqles/dashboard/`, `plugin/scripts/dashboard_server.py`, `plugin/tentaqles/demo/`, `plugin/tentaqles/snapshots/`, `plugin/templates/process/`, `plugin/tentaqles/prd.py`, `plugin/scripts/prd-status.py`, the skill writer of `plugin/tentaqles/skills.py` and its call in `plugin/scripts/memory-bridge.py`.
- **REQ-7**: The session preamble must keep working, and stop printing the PRD reading path, now its source is gone.
- **REQ-8**: Memory, vault export, ingest, profile, signals, the knowledge graph and embeddings must keep working: their tests still pass.
- **REQ-9**: `CLAUDE.md`'s Must not trade off must name, for each retired plugin test, the matching test of `internal/hookscript/`: `test_unreadable_payload_denies`, `test_tq_hook_turns_a_tq_without_decision_into_a_deny`, `test_tq_hook_stops_a_hanging_tq_and_denies`, `test_fallback_never_answers_ask`, `test_tq_hook_deadline_is_under_the_hook_timeout`, `test_tq_bin_is_not_honoured`, `test_tq_hook_never_runs_a_tq_from_the_plugin_folder`. `test_a_forged_pyc_next_to_the_sources_is_not_loaded` stays with the Python that remains.
- **REQ-10**: An identity rendered by ticket 002 must still deny a `.env` read after the plugin hook's removal, and an identity not rendered yet must be refused at launch by ticket 002's check, naming `tentaqles settings render`.
- **REQ-11**: PRD v2 must be amended, in §0, §1, §2, §5.1 and §10, to say the plugin holds the onboarding skill, session memory, the knowledge graph and the preamble, and to name who decided and when.
- **REQ-12**: `plugin/CLAUDE.md`, `plugin/README.md`, `plugin/HOW-TO.md`, `docs/COMMANDS.md` section 2, `docs/CLAUDE-HOOK.md`, `docs/threat-model.md` and `docs/prd.md` FR-18, FR-19 and FR-24 must match what is left.

## Out of scope

- Deleting anyone's existing `memory.db`, graph or vault.
- A separate plugin for memory: the owner chose to keep it in this one.
- Bumping the plugin version and the marketplace pin: the release flow does it.
- Removing tq CLI commands: ticket 015.

## Affected areas

- `plugin/skills/`, `plugin/agents/`, `plugin/hooks/hooks.json`, `plugin/scripts/`, `plugin/tentaqles/`, `plugin/templates/`
- `plugin/tests/`: remove tests of what's gone, and move the guard script's tests to Go in step with ticket 002
- `plugin/CLAUDE.md`, `plugin/README.md`, `plugin/HOW-TO.md`, `plugin/CHANGELOG.md`, `CHANGELOG.md`
- `internal/trust/`, `internal/cmd/integrity_test.go`, `internal/doctor/`: anything that names the plugin's PreToolUse entry
- `docs/COMMANDS.md`, `docs/CLAUDE-HOOK.md`, `docs/threat-model.md`, `docs/prd.md`, `docs/Tentaqles PRD v2 — Identity, Isolation, Guard.md`
- `docs/adr/0020-plugin-without-the-guard-hook.md`, `CLAUDE.md`

## Do not touch

- `plugin/skills/onboard/`: ticket 012 finished it.
- `plugin/tentaqles/memory/`, `plugin/tentaqles/graph/`, `plugin/tentaqles/embeddings/`, `plugin/tentaqles/metagraph/`: kept as they are, except for a removed import.
- `.claude/`: the repo's own build loop, which ships nothing.

## Decisions

- **Agreed with the owner**: keep memory and the knowledge graph.
- **Agreed with the owner**: delete the dev-process skills, the advisory skills, the 5 reviewer agents, the identity helpers, the dashboard and the demo.
- **Agreed with the owner by approving this ticket**: the plugin guard tests are retired, each replaced by the matching `internal/hookscript/` test named in `CLAUDE.md`. A replaced test isn't a weakened one, provided REQ-9's mapping is complete.
- **Decided by the ticket writer**: `snapshot-guard.py` goes with the `rollback` skill. It never fired anyway (DISCOVERY §5.4).
- **Decided by the ticket writer**: the self-modifying skill writer goes, per the owner's 2026-11-05 decision, which this doesn't reverse.
- **Decided by the ticket writer**: the integrity record still covers the plugin folder. It runs code through memory's hooks.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 014` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
