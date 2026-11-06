# 009: `status --json` for the skill, and `tentaqles connect <provider>`: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use fake CLIs for `verify`.

## Not covered

A live MCP connection needs a login, so it is covered by the onboarding skill's verify step, in ticket 012. Whether a vendor's hosted URL still answers is not checked at build time.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 status --json describes the identity
- **Covers**: REQ-1, REQ-2
- **Method**: test TestStatusJSON_Identity
- **Steps**:
  1. `go test ./internal/cmd -run '^TestStatusJSON_Identity$' -count=1`
- **Expect**: The object holds each top-level key, with one provider entry per scope member in manifest order, each with the listed fields.

### U-02 Connect adds the catalog's entry
- **Covers**: REQ-7, REQ-8, REQ-13
- **Method**: test TestConnect_WritesCatalogEntry
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_WritesCatalogEntry$' -count=1`
- **Expect**: `mcpServers` gains the exact entry, other keys are unchanged, the output reads `gh: connected as github`, and one log line carries no URL or command value.

### U-03 Connect twice
- **Covers**: REQ-9
- **Method**: test TestConnect_Idempotent
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_Idempotent$' -count=1`
- **Expect**: The second run prints `already connected`, with the file's bytes and modification time unchanged.

### U-04 No server in the catalog
- **Covers**: REQ-11
- **Method**: test TestConnect_NoEntry
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_NoEntry$' -count=1`
- **Expect**: `az: no MCP server in the catalog`, exit 0, no write.

### F-01 Neutral, untrusted and off
- **Covers**: REQ-3, REQ-4, REQ-12
- **Method**: test TestStatusAndConnect_BadStates
- **Steps**:
  1. `go test ./internal/cmd -run '^TestStatusAndConnect_BadStates$' -count=1`
- **Expect**: `status --json` exits 0 with null identity, or `trusted` false with a reason and no providers; `connect` exits 1 naming each state and writes nothing.

### F-02 A damaged config
- **Covers**: REQ-8
- **Method**: test TestConnect_DamagedClaudeJson
- **Steps**:
  1. `go test ./internal/connect -run '^TestConnect_DamagedClaudeJson$' -count=1`
- **Expect**: On invalid JSON or an `mcpServers` of the wrong type, it exits 1 naming the file and leaves the bytes untouched. On a missing file it creates one holding only `mcpServers`.

### F-03 Verify hangs or fails
- **Covers**: REQ-2
- **Method**: test TestStatusJSON_VerifyBounded
- **Steps**:
  1. `go test ./internal/identitystatus -run '^TestStatusJSON_VerifyBounded$' -count=1`
- **Expect**: A hanging fake CLI gives `verified` false within the 5 s limit, and a failing one gives false.

### Q-01 Concurrent writes
- **Covers**: REQ-8
- **Method**: test TestConnect_AtomicWrite
- **Steps**:
  1. `go test ./internal/connect -run '^TestConnect_AtomicWrite$' -count=1`
- **Expect**: Eight runs at once for different providers leave valid JSON holding all eight, through a temporary file and rename.

### Q-02 Docs
- **Covers**: REQ-17
- **Method**: inspect
- **Steps**:
  1. Read `docs/COMMANDS.md`.
- **Expect**: Both commands are described, and the difference between `status --json` and `doctor --json` is stated.

### R-01 Doctor JSON is unchanged
- **Covers**: REQ-6
- **Method**: test TestDoctor_JSONFlagPrintsFindings
- **Steps**:
  1. `go test ./internal/cmd ./internal/doctor -count=1`
- **Expect**: `ok`, and that test is unchanged.

### R-02 MCP rules accept what connect wrote
- **Covers**: REQ-16
- **Method**: test TestConnect_SurvivesRenderAndHook
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_SurvivesRenderAndHook$' -count=1`
- **Expect**: After `connect` and `tq settings render`, the entry remains, and a hook call to its tool is allowed in that identity and denied in another.

### S-01 Out of scope is refused and logged
- **Covers**: REQ-10
- **Method**: test TestConnect_OutOfScopeRefused
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_OutOfScopeRefused$' -count=1`
  2. On an Azure-only identity, run `tentaqles connect gh`, with a marker set for an agent caller.
- **Expect**: Exit 1 naming the scope, the file unchanged, and one denial log line with category `identity`.

### S-02 An agent can't choose the entry
- **Covers**: REQ-14
- **Method**: test TestConnect_NoCallerInput
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_NoCallerInput$' -count=1`
  2. Run `tentaqles connect gh --url http://evil`, `tentaqles connect gh evil` and `tentaqles connect "gh;x"`, and add a user provider file overriding `gh`'s `connect:`.
- **Expect**: Each flag and extra argument refuses, and the user file's value never reaches `.claude.json`.

### S-03 No secret in status
- **Covers**: REQ-5
- **Method**: test TestStatusJSON_NoSecrets
- **Steps**:
  1. `go test ./internal/cmd -run '^TestStatusJSON_NoSecrets$' -count=1`
  2. Put a token in the keychain seam, an MCP `env` value, and a provider variable, each with a marker string.
- **Expect**: The output doesn't contain any marker string, and `credential` reads `present`.

### S-04 File tools stay denied while connect passes
- **Covers**: REQ-15
- **Method**: test TestPreToolUse_ConnectAllowedFileToolsDenied
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_ConnectAllowedFileToolsDenied$' -count=1`
  2. Feed `tentaqles connect gh`, then a Write and a shell redirect on the identity's `.claude.json`.
- **Expect**: The first is allowed, the other two denied with `BLOCKED [self-protection/tq/protected-path]`.

### S-05 Log has no values
- **Covers**: REQ-13
- **Method**: test TestConnect_LogRedacted
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_LogRedacted$' -count=1`
- **Expect**: The log line holds the five named fields only.
