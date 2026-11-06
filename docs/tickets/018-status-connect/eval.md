# 018: `status --json` for the skill, and `tentaqles connect <provider>`: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use fake CLIs for `verify`.

## Not covered

A live MCP connection needs a login, so the owner checks it in the end-to-end run. Whether a vendor's URL still answers isn't checked at build time.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 status --json describes the client
- **Covers**: REQ-1, REQ-2
- **Method**: test TestStatusJSON_Identity
- **Steps**:
  1. `go test ./internal/cmd -run '^TestStatusJSON_Identity$' -count=1`
- **Expect**: The object holds each top-level key, with one entry per provider in scope, each with the listed fields.

### U-02 Connect adds the catalog's entry
- **Covers**: REQ-8, REQ-9, REQ-14
- **Method**: test TestConnect_WritesCatalogEntry
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_WritesCatalogEntry$' -count=1`
- **Expect**: `mcpServers` gains the exact entry, other keys are unchanged, the output reads `connected as`, and one log line carries no URL or command value.

### U-03 Connect twice, and no entry
- **Covers**: REQ-10, REQ-12
- **Method**: test TestConnect_IdempotentAndNoEntry
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_IdempotentAndNoEntry$' -count=1`
- **Expect**: The second run prints `already connected` with the file's bytes unchanged, and `az` prints `no MCP server in the catalog`, exit 0.

### F-01 Neutral and untrusted
- **Covers**: REQ-3, REQ-4, REQ-13
- **Method**: test TestStatusAndConnect_BadStates
- **Steps**:
  1. `go test ./internal/cmd -run '^TestStatusAndConnect_BadStates$' -count=1`
- **Expect**: `status --json` exits 0 with a null identity, or `trusted` false with a reason and no providers; `connect` exits 1 naming each state and writes nothing.

### F-02 A damaged config
- **Covers**: REQ-9
- **Method**: test TestConnect_DamagedClaudeJson
- **Steps**:
  1. `go test ./internal/connect -run '^TestConnect_DamagedClaudeJson$' -count=1`
- **Expect**: On invalid JSON or an `mcpServers` of the wrong type it exits 1 naming the file and leaves the bytes untouched. On a missing file it creates one.

### F-03 Verify hangs or fails
- **Covers**: REQ-6
- **Method**: test TestStatusJSON_VerifyBounded
- **Steps**:
  1. `go test ./internal/identitystatus -run '^TestStatusJSON_VerifyBounded$' -count=1`
- **Expect**: A hanging fake gives `verified` false within the limit, and a failing one gives false.

### Q-01 Concurrent writes
- **Covers**: REQ-9
- **Method**: test TestConnect_AtomicWrite
- **Steps**:
  1. `go test ./internal/connect -run '^TestConnect_AtomicWrite$' -count=1`
- **Expect**: Eight runs at once leave valid JSON holding all eight.

### R-01 Doctor JSON is unchanged
- **Covers**: REQ-7
- **Method**: test TestDoctor_JSONFlagPrintsFindings
- **Steps**:
  1. `go test ./internal/cmd ./internal/doctor -run 'TestDoctor' -count=1`
- **Expect**: `ok`, with that test unchanged.

### R-02 The MCP rules accept what connect wrote
- **Covers**: REQ-17
- **Method**: test TestConnect_SurvivesRenderAndHook
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_SurvivesRenderAndHook$' -count=1`
- **Expect**: After `connect` and a render the entry remains, and a hook call to its tool is allowed in that client and denied in another.

### S-01 Out of scope is refused and logged
- **Covers**: REQ-11
- **Method**: test TestConnect_OutOfScopeRefused
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_OutOfScopeRefused$' -count=1`
- **Expect**: On an Azure-only client, `tentaqles connect gh` exits 1 naming the scope, the file is unchanged, and the denial log has one line with category `identity`.

### S-02 An agent can't choose the entry
- **Covers**: REQ-15
- **Method**: test TestConnect_NoCallerInput
- **Steps**:
  1. `go test ./internal/cmd -run '^TestConnect_NoCallerInput$' -count=1`
  2. Run `tentaqles connect gh --url http://evil`, `tentaqles connect gh evil`, and add a user file overriding `gh`'s `connect:`.
- **Expect**: Each flag and extra argument refuses, and the user file's value never reaches `.claude.json`.

### S-03 No secret in status
- **Covers**: REQ-5
- **Method**: test TestStatusJSON_NoSecrets
- **Steps**:
  1. `go test ./internal/cmd -run '^TestStatusJSON_NoSecrets$' -count=1`
- **Expect**: With marker strings in the keychain seam, an MCP `env` value and a provider variable, the output holds none of them, and `credential` reads `present`.

### S-04 File tools stay denied while connect passes
- **Covers**: REQ-16
- **Method**: test TestPreToolUse_ConnectAllowedFileToolsDenied
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_ConnectAllowedFileToolsDenied$' -count=1`
- **Expect**: `tentaqles connect gh` is allowed, and a Write and a shell redirect on the client's `.claude.json` are denied with `BLOCKED [self-protection/tq/protected-path]`.
