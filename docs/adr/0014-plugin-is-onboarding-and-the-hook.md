# ADR-0014: The plugin is the onboarding skill and the guard hook

Status: accepted
Date: 2026-11-06
Cites: `docs/IDEATION.md`, ADR-0006 (deny only, fail closed)

## Context

The plugin held 40 skills, 5 agents, session memory, a knowledge graph, a dashboard and about 30,000
lines of Python. On 2026-11-06 the owner reset the product to IDEATION and chose to remove everything
else for now, memory and the graph included. This supersedes the same day's earlier decision to keep them.

## Decision

The plugin keeps `skills/onboard`, `hooks/hooks.json` and `scripts/tq_hook.sh`. Everything else goes, all
its Python included.

With the Python fallback gone, a missing tq denies **every** tool as `tq/guard-error`. The fallback used
to judge shell commands and allow other tools, and with no Python at all it let non-shell tools through.
This is stricter. A machine without tq cannot use a session that has this plugin, and the message says so.

## Consequences

- Existing `memory.db` files, graphs and vaults stay on disk. Nothing reads them after an upgrade, and
  nothing deletes them. The removed code remains in git history.
- The hook still comes only from the plugin. A client without the plugin has no guard. That gap is
  separate and still open.
- With the owner's approval, the must-not-trade-off tests of the removed Python are gone:
  `test_unreadable_payload_denies`, `test_fallback_never_answers_ask` and
  `test_a_forged_pyc_next_to_the_sources_is_not_loaded`. The behaviour they guarded no longer exists.
  `plugin/tests/test_tq_hook.py` keeps the hook's own: `test_tq_bin_is_not_honoured`,
  `test_tq_hook_never_runs_a_tq_from_the_plugin_folder`, `test_tq_hook_turns_a_tq_without_decision_into_a_deny`,
  `test_tq_hook_stops_a_hanging_tq_and_denies` and `test_tq_hook_deadline_is_under_the_hook_timeout`.
- An upgrade changes the plugin folder the integrity record covers, so each workspace needs one
  `tq hooks verify --accept`.
