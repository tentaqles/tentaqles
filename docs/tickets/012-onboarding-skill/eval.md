# 012: The onboarding skill: checks

Requirements: [rd.md](rd.md)

## Setup

`PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q` runs the text checks. The owner's check needs a real identity.

## Not covered

An LLM's reading of the skill can't be proven by a text test: the tests prove what it says and U-02 proves it works once. Enforcement is the hook's job, proven by earlier tickets.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 The walk is written down
- **Covers**: REQ-1, REQ-3, REQ-4, REQ-5, REQ-6, REQ-7, REQ-8, REQ-11, REQ-12, REQ-13, REQ-15
- **Method**: test test_onboard_skill_walks_every_step
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k walks_every_step`
- **Expect**: The text names `tentaqles status --json`, `tentaqles install`, `tentaqles connect`, the three credential methods in order, the `!` prefix, the five table columns, and the description names each trigger.

### U-02 The owner onboards a client
- **Covers**: REQ-3, REQ-7, REQ-12, REQ-16
- **Method**: human
- **Steps**:
  1. After `tentaqles reach scratch-client` with GitHub and one MCP provider, log in to Claude Code and let the skill run.
- **Expect**: The skill starts on its own, reaches every provider, and ends with the table, all verified.

### U-03 A re-run skips what's done
- **Covers**: REQ-13
- **Method**: human
- **Steps**:
  1. Run `/tentaqles:onboard` again right after U-02, then uninstall one CLI and run it again.
- **Expect**: The first re-run only verifies; the second reinstalls only that CLI.

### F-01 Neutral or untrusted folder
- **Covers**: REQ-2
- **Method**: test test_onboard_skill_stops_when_not_an_identity
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k stops_when_not_an_identity`
- **Expect**: The text says to stop in both cases, and names `tentaqles reach` and `tq hooks verify`.

### F-02 Manual install
- **Covers**: REQ-4
- **Method**: test test_onboard_skill_never_picks_an_installer
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k never_picks_an_installer`
- **Expect**: The text has no `winget`, `brew`, `apt`, `npm install`, `pip install`, `.msi`, `.exe` or `curl`, and says to give the vendor's page for `manual`.

### F-03 A connector only the developer can enable
- **Covers**: REQ-6
- **Method**: test test_onboard_skill_handles_no_catalog_server
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k handles_no_catalog_server`
- **Expect**: The text covers `no MCP server in the catalog` and a claude.ai connector, gives the developer the step, and checks afterwards.

### Q-01 The skill stays short
- **Covers**: REQ-1
- **Method**: command
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_skill_frontmatter.py -q`
  2. Count the lines of `plugin/skills/onboard/SKILL.md`.
- **Expect**: The frontmatter test passes, and the file holds at most 150 lines.

### R-01 Its name is stable
- **Covers**: REQ-16
- **Method**: inspect
- **Steps**:
  1. Read the skill's frontmatter, and search `internal/reach/` for the first prompt.
- **Expect**: The name is `onboard`, and the prompt invokes `/tentaqles:onboard`.

### R-02 No commands from the old flow remain
- **Covers**: REQ-1
- **Method**: test test_onboard_skill_has_no_v1_flow
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k has_no_v1_flow`
- **Expect**: The text has no `tq doctor --json`, `tq bundle sync`, `tq allow` or `tq onboard`.

### S-01 It never reads protected files
- **Covers**: REQ-1
- **Method**: test test_onboard_skill_reads_no_protected_path
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k reads_no_protected_path`
- **Expect**: No instruction reads `.tentaqles.yaml`, `manifest.yaml`, `.claude.json`, `~/.tentaqles`, or the catalog file.

### S-02 Tokens stay out of the transcript
- **Covers**: REQ-10
- **Method**: test test_onboard_skill_never_handles_a_token
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k never_handles_a_token`
- **Expect**: The text has `Never` rules for asking, repeating and storing a token, tells what to do with a pasted one, and names `tentaqles secret set`.

### S-03 Scope is refused
- **Covers**: REQ-14
- **Method**: test test_onboard_skill_refuses_out_of_scope
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k refuses_out_of_scope`
- **Expect**: The text says to refuse a provider outside `scope` and names `tentaqles edit`.

### S-04 The agent's route to a browser
- **Covers**: REQ-9
- **Method**: test test_onboard_skill_opens_no_browser_directly
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k opens_no_browser_directly`
- **Expect**: The text says to use the identity's browser shim, and has no `start http`, `open http`, `chrome` or `msedge` command.

### S-05 The guard holds if the skill is ignored
- **Covers**: REQ-14
- **Method**: test TestPreToolUse_ScopeReferenceScenario
- **Steps**:
  1. `go test ./internal/cmd -run 'TestPreToolUse_ScopeReferenceScenario|TestInstall_OutOfScopeRefused|TestConnect_OutOfScopeRefused' -count=1`
- **Expect**: All pass, with no change to their files in this diff.
