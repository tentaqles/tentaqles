# 021: The onboarding skill drives the logins: checks

Requirements: [rd.md](rd.md)

## Setup

`PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q` runs the text checks. On Windows use Git Bash or set `BASH`. The owner's checks need a real client.

## Not covered

How a model reads the skill can't be proven by a text test. The tests prove what it says, and the owner's U-02 proves it works once. Enforcement is the hook's job.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 The walk is written down
- **Covers**: REQ-1, REQ-3, REQ-4, REQ-5, REQ-6, REQ-8, REQ-9, REQ-12, REQ-13, REQ-14, REQ-16
- **Method**: test test_onboard_skill_walks_every_step
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k walks_every_step`
- **Expect**: The text names `tentaqles status --json`, `tentaqles install`, `tentaqles connect`, the three credential methods in order, the `!` prefix and the five table columns, and the description names each trigger.

### U-02 The owner onboards a client
- **Covers**: REQ-3, REQ-9, REQ-13
- **Method**: human
- **Steps**:
  1. In a scratch client with GitHub and one MCP provider, log in to Claude Code and run `/tentaqles:onboard`.
- **Expect**: It reaches every provider and ends with the table, all verified.

### F-01 Neutral or untrusted folder
- **Covers**: REQ-2
- **Method**: test test_onboard_skill_stops_when_not_a_client
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k stops_when_not_a_client`
- **Expect**: The text says to stop in both cases, naming `tentaqles reach` and `tq hooks verify`.

### F-02 Manual install
- **Covers**: REQ-4
- **Method**: test test_onboard_skill_never_picks_an_installer
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k never_picks_an_installer`
- **Expect**: The text has no `winget`, `brew`, `apt`, `npm install`, `pip install`, `.msi`, `.exe` or `curl`, and says to give the vendor's page for `manual`.

### F-03 No catalog server
- **Covers**: REQ-8
- **Method**: test test_onboard_skill_handles_no_catalog_server
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k handles_no_catalog_server`
- **Expect**: The text covers `no MCP server in the catalog` and a claude.ai connector, gives the developer the step, and checks afterwards.

### Q-01 The skill stays short
- **Covers**: REQ-1
- **Method**: test test_onboard_skill_is_short
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k is_short`
- **Expect**: The file holds at most 150 lines and has valid frontmatter.

### R-01 No old flow remains
- **Covers**: REQ-1
- **Method**: test test_onboard_skill_has_no_old_flow
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k has_no_old_flow`
- **Expect**: The text has no `tq doctor --json`, `tq bundle sync`, `tq allow` or `tq onboard`.

### R-02 The plugin still holds only this
- **Covers**: REQ-1
- **Method**: test test_the_plugin_holds_only_the_onboarding_skill_and_the_guard_hook
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_tq_hook.py -q -k holds_only`
- **Expect**: Passes.

### S-01 It never reads protected files
- **Covers**: REQ-1
- **Method**: test test_onboard_skill_reads_no_protected_path
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k reads_no_protected_path`
- **Expect**: No instruction reads `.tentaqles.yaml`, `manifest.yaml`, `.claude.json` or `~/.tentaqles`.

### S-02 Tokens stay out of the transcript
- **Covers**: REQ-7
- **Method**: test test_onboard_skill_never_handles_a_token
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k never_handles_a_token`
- **Expect**: The text has rules against asking for, repeating and storing a token, says what to do with a pasted one, and names `tq secret set`.

### S-03 Scope and browser
- **Covers**: REQ-10, REQ-15
- **Method**: test test_onboard_skill_keeps_scope_and_browser
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k keeps_scope_and_browser`
- **Expect**: The text says to refuse a provider outside `scope`, names `tentaqles open`, and has no `start http`, `open http`, `chrome` or `msedge` command.

### S-04 Other providers are honest
- **Covers**: REQ-11
- **Method**: test test_onboard_skill_says_other_is_not_isolated
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_onboard_skill.py -q -k says_other_is_not_isolated`
- **Expect**: The text says an `other_providers` login isn't isolated per client and the guard can't check it.

### S-05 The guard holds if the skill is ignored
- **Covers**: REQ-15
- **Method**: test TestInstall_OutOfScopeRefused
- **Steps**:
  1. `go test ./internal/cmd -run 'TestPreToolUse_ScopeReferenceScenario|TestInstall_OutOfScopeRefused|TestConnect_OutOfScopeRefused' -count=1`
- **Expect**: All pass, with no change to their files in this diff.
