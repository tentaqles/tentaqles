# 006: `tentaqles off` and `tentaqles on`: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use `internal/cmd/main_test.go`'s human baseline with the `IsTerminal`, `ParentOf` and `Confirm` seams. A fake `claude` records its environment. The integrity key uses the test keychain or the plain-file fallback, as `internal/trust`'s tests do.

## Not covered

A live Claude Code session is not started, so REQ-12 is proven at the hook level and by the owner's U-06.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 Off makes this terminal raw
- **Covers**: REQ-2, REQ-3, REQ-6
- **Method**: test TestOff_CurrentTerminalBecomesRaw
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_CurrentTerminalBecomesRaw$' -count=1`
- **Expect**: After `yes`, the next `tq env` output restores the pre-identity PATH byte for byte, unsets the provider variables and `CLAUDE_CONFIG_DIR`, and sets the marker.

### U-02 A new terminal is raw
- **Covers**: REQ-7, REQ-14
- **Method**: test TestOff_NewTerminalIsRawAndRegisters
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_NewTerminalIsRawAndRegisters$' -count=1`
- **Expect**: `tq activate` on a human terminal adds its id to the signed list, and the output is the banner with `guard: off` and the exact warning line.

### U-03 An open terminal keeps its state
- **Covers**: REQ-9
- **Method**: test TestOff_OpenTerminalKeepsIdentity
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_OpenTerminalKeepsIdentity$' -count=1`
- **Expect**: A terminal with identity state and no marker gets the identity environment at each prompt, and the hook denies a `.env` read in it after `off` ran elsewhere.

### U-04 On restores this terminal
- **Covers**: REQ-5, REQ-15
- **Method**: test TestOn_RestoresTheTerminal
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOn_RestoresTheTerminal$' -count=1`
- **Expect**: `guard on for <identity>` prints, the off file is gone, and the next `tq env` gives the identity's PATH and variables.

### U-05 Claude in a raw session
- **Covers**: REQ-13
- **Method**: test TestOff_ClaudeInRawSession
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_ClaudeInRawSession$' -count=1`
- **Expect**: The fake claude records `--dangerously-skip-permissions` and no `CLAUDE_CONFIG_DIR`.

### U-06 The owner runs one session
- **Covers**: REQ-12, REQ-13
- **Method**: human
- **Steps**:
  1. On a flagged identity, run `tentaqles off`, then `claude`. Ask it to read a `.env` in a test folder, exit, then run `tentaqles on`.
- **Expect**: Claude Code uses the global login and the read is not blocked; after `on` a new session has the identity login and the read is blocked.

### U-07 Status shows it
- **Covers**: REQ-19
- **Method**: test TestDoctor_ShowsGuardOff
- **Steps**:
  1. `go test ./internal/cmd ./internal/doctor -run '^TestDoctor_ShowsGuardOff$' -count=1`
- **Expect**: `tq doctor` and `tq list` show `guard: off` and a count, and no output contains a session id.

### F-01 A forged or damaged state counts as on
- **Covers**: REQ-4
- **Method**: test TestOffState_ForgedCountsAsOn
- **Steps**:
  1. `go test ./internal/offstate ./internal/cmd -run '^TestOffState_ForgedCountsAsOn$' -count=1`
- **Expect**: Unsigned JSON, a flipped byte, a signature from another key, a list edited after signing, an empty file and a directory in its place each give the identity environment and a hook that denies.

### F-02 The keychain is unavailable
- **Covers**: REQ-1, REQ-4
- **Method**: test TestOff_KeyUnavailable
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_KeyUnavailable$' -count=1`
- **Expect**: `off` fails with a line naming the signing key and writes nothing; the guard stays on.

### F-03 Off on an unflagged identity
- **Covers**: REQ-18
- **Method**: test TestOff_RefusesWithoutFlag
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_RefusesWithoutFlag$' -count=1`
- **Expect**: Non-zero exit, a line naming `guard: v2` and `tentaqles edit`, and no new file.

### F-04 Unknown identity, neutral folder, and two at once
- **Covers**: REQ-1, REQ-3
- **Method**: test TestOff_BadTargetsAndConcurrency
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_BadTargetsAndConcurrency$' -count=1`
- **Expect**: Unknown name and a neutral folder with no name exit non-zero naming the problem; ten terminals registering at once leave a valid signature and all ten ids.

### Q-01 The hook stays bounded
- **Covers**: REQ-11
- **Method**: test BenchmarkPreToolUse_OffStateCheck
- **Steps**:
  1. `go test ./internal/cmd -run '^$' -bench '^BenchmarkPreToolUse_OffStateCheck$' -benchtime=50x`
- **Expect**: The added check costs under 5 ms per call with no off state, and under 20 ms with one, and adds no subprocess.

### R-01 Other identities are untouched
- **Covers**: REQ-17
- **Method**: test TestOff_OtherIdentitiesUnaffected
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_OtherIdentitiesUnaffected$' -count=1`
- **Expect**: After `off` on A, B's `tq env` and hook decisions are byte-identical to before, including when a raw terminal of A `cd`s into B.

### R-02 Named guard tests stay
- **Covers**: REQ-11
- **Method**: test TestPreToolUse_ScopeReferenceScenario
- **Steps**:
  1. `go test ./internal/cmd ./internal/policy ./internal/trust -count=1`
  2. `PYTHONPATH=plugin python -m pytest plugin/tests/test_tq_off.py -q`
- **Expect**: All exit 0, with no named test of `CLAUDE.md` weakened.

### S-01 An agent cannot run off
- **Covers**: REQ-1
- **Method**: test TestOff_RefusesAgents
- **Steps**:
  1. `go test ./internal/cmd ./internal/policy -run '^TestOff_RefusesAgents$' -count=1`
  2. Run it under each detector alone, and feed the hook `tentaqles off`, `tq off`, `tq.exe off acme` and `C:\x\tentaqles.exe off`.
- **Expect**: Each refuses with `human-only`, and each hook call is denied with `BLOCKED [self-protection/tq/human-only-command]`; no off file exists and a refusal is logged.

### S-02 A marker alone does nothing
- **Covers**: REQ-10, REQ-11
- **Method**: test TestOff_MarkerAloneDoesNothing
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_MarkerAloneDoesNothing$' -count=1`
  2. Set `TQ_OFF_SESSION` to a guessed id, with no off state; then with an off state and an id not in it; then with an off state for A and a marker naming B; then with a valid marker for A in B's folder.
- **Expect**: Every case runs the rules, and a `.env` read is denied.

### S-03 An agent's new shell cannot register
- **Covers**: REQ-7, REQ-8
- **Method**: test TestOff_AgentShellCannotRegister
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOff_AgentShellCannotRegister$' -count=1`
  2. With the identity off, run `tq activate` with `CLAUDECODE=1`, then with a `claude` parent process through the `ParentOf` seam.
- **Expect**: The signed list is unchanged, the emitted code gives the identity environment, and the hook denies a `.env` read in that shell.

### S-04 An agent cannot read or write the state
- **Covers**: REQ-3, REQ-4
- **Method**: test TestProtectedPaths_OffState
- **Steps**:
  1. `go test ./internal/cmd -run '^TestProtectedPaths_OffState$' -count=1`
  2. Feed the hook Read, Write and Grep calls, `cat`, `cp` and a redirect on the off file.
- **Expect**: Each is denied with `BLOCKED [self-protection/tq/protected-path]`.

### S-05 On brings the guard back everywhere at once
- **Covers**: REQ-16
- **Method**: test TestOn_RawTerminalGuardBackAtOnce
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOn_RawTerminalGuardBackAtOnce$' -count=1`
- **Expect**: With a marker that was valid before `on`, the next hook call denies a `.env` read, while that terminal's environment is unchanged.

### S-06 An agent may run on
- **Covers**: REQ-5
- **Method**: test TestOn_AgentAllowed
- **Steps**:
  1. `go test ./internal/cmd ./internal/policy -run '^TestOn_AgentAllowed$' -count=1`
- **Expect**: With `CLAUDECODE=1` and no terminal, `tentaqles on` succeeds, and the hook allows the command.

### S-07 Limits are written down
- **Covers**: REQ-20
- **Method**: inspect
- **Steps**:
  1. Read `docs/COMMANDS.md`, `docs/adr/0014-guard-off-state.md`, `docs/threat-model.md`.
- **Expect**: Each §9.4 limit appears, and the threat model names the off state and its marker.
