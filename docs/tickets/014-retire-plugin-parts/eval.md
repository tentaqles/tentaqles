# 014: Cut the plugin down to onboarding, memory and the graph: checks

Requirements: [rd.md](rd.md)

## Setup

Plugin suite: `PYTHONPATH=plugin python -m pytest plugin/tests -q`, about 13 minutes on Windows. Go tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`.

## Not covered

An existing user's plugin upgrade runs through the release flow and `tq hooks verify --accept`, and the owner checks it at the next release.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 The skills are exactly the twelve
- **Covers**: REQ-1, REQ-2
- **Method**: test test_skill_set_is_exactly_the_kept_twelve
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_skill_frontmatter.py -q`
- **Expect**: Passes, and the test compares the folder list to the 12 names.

### U-02 The agents are gone
- **Covers**: REQ-3
- **Method**: command
- **Steps**:
  1. `git ls-files plugin/agents`
- **Expect**: No output.

### U-03 The removed code is gone
- **Covers**: REQ-5, REQ-6
- **Method**: command
- **Steps**:
  1. `git ls-files plugin/tentaqles/dashboard plugin/tentaqles/demo plugin/tentaqles/snapshots plugin/templates/process plugin/tentaqles/prd.py plugin/scripts/prd-status.py plugin/scripts/dashboard_server.py plugin/scripts/identity-guard.py plugin/scripts/snapshot-guard.py`
- **Expect**: No output.

### U-04 The PRD says what the plugin holds
- **Covers**: REQ-11
- **Method**: inspect
- **Steps**:
  1. Read §0, §1, §2, §5.1 and §10 of `docs/Tentaqles PRD v2 — Identity, Isolation, Guard.md`.
- **Expect**: Each names the onboarding skill, session memory, the knowledge graph and the preamble, and a revision line records the owner's decision.

### F-01 tq_hook.sh refuses the retired event
- **Covers**: REQ-5
- **Method**: test test_tq_hook_refuses_the_retired_events
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_identity_guard.py -q -k refuses_the_retired_events`
- **Expect**: Passes with `pre-tool-use` in its list of events, each exiting 2.

### F-02 An identity not rendered yet
- **Covers**: REQ-10
- **Method**: test TestRun_RefusesWithNoGuardHook
- **Steps**:
  1. `go test ./internal/cmd -run 'TestRun_RefusesWithNoGuardHook|TestDoctor_GuardHookMissing' -count=1`
  2. `go test ./internal/doctor -run 'TestDoctor_GuardHookMissing' -count=1`
- **Expect**: All pass, with the fix line naming `tentaqles settings render`.

### F-03 The preamble without a PRD
- **Covers**: REQ-7
- **Method**: test test_default_output_contains_client_header
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests -q -k "default_output_contains_client_header or compact"`
- **Expect**: Pass, and the output carries no PRD reading path.

### Q-01 Docs match the tree
- **Covers**: REQ-12
- **Method**: test TestCommandsDocMatchesTree
- **Steps**:
  1. `go test ./internal/cmd -run '^TestCommandsDocMatchesTree$' -count=1`
- **Expect**: Section 2 of `docs/COMMANDS.md` names each kept skill and no other.

### Q-02 No dangling references
- **Covers**: REQ-2, REQ-12
- **Method**: command
- **Steps**:
  1. `git grep -nE "tentaqles:(ship|feature|fix|kickoff|dod|direction|prd|babysit-pr|grill-me|add-client|client-settings|switch-client|workspace-status|rollback|dashboard)\b" -- plugin docs/COMMANDS.md README.md`
- **Expect**: No output.

### R-01 Memory and the graph still work
- **Covers**: REQ-8
- **Method**: test test_search_memory
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests -q`
- **Expect**: Exit 0, with `test_search_memory.py`, `test_embeddings_optional.py`, `test_signals.py` and the graph tests present and passing.

### R-02 Hooks stay bounded
- **Covers**: REQ-4
- **Method**: test test_every_hook_has_a_timeout
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests -q -k "every_hook_has_a_timeout or hooks_json"`
- **Expect**: Pass, and hooks.json has no PreToolUse key and every kept hook has a timeout.

### R-03 The Go gates pass
- **Covers**: REQ-10
- **Method**: command
- **Steps**:
  1. `go build ./...`
  2. `go vet ./...`
  3. `go test ./... -count=1`
- **Expect**: Each exits 0.

### S-01 The guard works with the plugin's hook gone
- **Covers**: REQ-10
- **Method**: test TestHookScript_GuardWorksWithoutPlugin
- **Steps**:
  1. `go test ./internal/cmd -run '^TestHookScript_GuardWorksWithoutPlugin$' -count=1`
- **Expect**: A `.env` read through the rendered hook gives exit 2 and `BLOCKED [secrets/tq/env-file-read]`.

### S-02 Every retired guard test has a replacement
- **Covers**: REQ-9
- **Method**: inspect
- **Steps**:
  1. Read `git diff main -- CLAUDE.md`, then for each test named in REQ-9 find the replacement it names in `internal/hookscript/`, and read that test.
- **Expect**: Each one exists and asserts the same behaviour, and no other named test left.

### S-03 The integrity record still covers the plugin
- **Covers**: REQ-8
- **Method**: test TestPluginHookChangesAreNamed
- **Steps**:
  1. `go test ./internal/trust ./internal/cmd -run 'TestPluginHookChangesAreNamed|TestCheckReleaseComparesBytesNotLocation|TestARepinnedCloneIsRefused' -count=1`
- **Expect**: All pass, and a changed plugin hook still makes the identity untrusted.

### S-04 Forged bytecode stays refused
- **Covers**: REQ-9
- **Method**: test test_a_forged_pyc_next_to_the_sources_is_not_loaded
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests -q -k forged_pyc`
- **Expect**: Passes.
