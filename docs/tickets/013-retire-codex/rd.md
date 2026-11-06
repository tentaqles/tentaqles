# 013: Retire Codex support

**Status:** parked
**Risk:** High: removing Codex code touches the guard, trust and protected paths, and a slip can drop a check Claude Code sessions rely on, or make every existing identity's signed record read as changed
**UI:** none

PRD v2 §10: Claude Code is the only harness, and Tentaqles ships no shims, hooks or config for any other. This removes tq's Codex code and leaves the guard for Claude Code exactly as strong as before. A Codex process remains something the human-only check recognises as an agent, since a person may still have Codex installed.

## Requirements

- **REQ-1**: The `codex` and `codex-off` functions must be gone from every shell hook, and `tq run . -- codex` must run `codex` as any other command, with no injected flag.
- **REQ-2**: `tq settings render` must stop writing `$CODEX_HOME/hooks.json` and any `config.toml` block, and must print one line naming an old Codex file it found, and leave it in place.
- **REQ-3**: `tq claude-hook pre-tool-use --agent codex` must deny as `tq/guard-error`, naming that Codex is not supported, as RunMain already does for a flag tq does not know.
- **REQ-4**: The `apply_patch` parsing and the Codex hook answer format must be gone from the hook.
- **REQ-5**: `tq doctor` must stop running Codex checks (`codex-hooks-missing`, `codex-hooks-untrusted`).
- **REQ-6**: The integrity record must stop covering the Codex files, and an identity signed before this change must still verify, without `tq hooks verify --accept`, when its only difference is those keys.
- **REQ-7**: The protected path set must stop adding Codex dirs from `CODEX_HOME`, while `~/.tentaqles/identities/*/codex` stays protected as part of the state folder.
- **REQ-8**: `internal/providers/catalog/codex.yaml` must stay, as the entry that makes `codex` an out-of-scope CLI, with its match lists unchanged.
- **REQ-9**: `internal/markers` and the process-tree detector must keep recognising Codex, so a human-only command under Codex still refuses.
- **REQ-10**: The MCP filter must drop its Codex home path, and leave Claude's unchanged.
- **REQ-11**: `CLAUDE.md`'s Must not trade off must drop or rename the Codex-only tests: `TestProtectedPaths_Codex`, `TestCodexHookResolves`, `TestLaunchArgsInjectsTheBypassFlag`, `TestFilterCodex`, `TestFilterCodex_CredentialFields`, and `TestPreToolUseIncompleteScanDeniesBothAgents`, which becomes `TestPreToolUseIncompleteScanDenies`, keeping the Claude half.
- **REQ-12**: `README.md`, `docs/COMMANDS.md`, `docs/CLAUDE-SETTINGS.md`, `docs/CLAUDE-HOOK.md`, `docs/prd.md` FR-23 and FR-31, and `docs/threat-model.md` must no longer offer Codex as supported.

## Out of scope

- Deleting a user's `~/.tentaqles/identities/<ws>/codex` folder. It stays on disk.
- OpenCode, Kimi and other agent entries in the catalog: they stay as out-of-scope matches.
- `agents:` in the manifest: it still loads, and ticket 015 doesn't change it either.

## Affected areas

- `internal/codexcfg/`: delete
- `internal/cmd/run_codex.go`, `codex_render.go`, `claudehook_codex.go`, `claudehook_patch.go`: delete
- `internal/cmd/run.go`, `settings.go`, `scope_render.go`, `claudehook*.go`, `allow.go`, `add.go`, `hooks_verify.go`: remove the Codex paths
- `internal/doctor/codex.go`, `internal/doctor/launcher.go`, `internal/doctor/registry.go`
- `internal/mcpscope/codex.go`, `internal/mcpscope/mcpscope.go`
- `internal/trust/covered.go`, `hash.go`, `trust.go`
- `internal/protect/pathset.go`, `internal/policy/mcpentry.go`, `internal/shell/hooks.go`, `internal/shell/hooks/*.tmpl`, `internal/shell/testdata/*`, `internal/envplan/off.go`, `internal/audit/audit.go`
- docs named above, `CHANGELOG.md`, `CLAUDE.md`, `docs/adr/0019-claude-code-only.md`

## Do not touch

- `internal/markers/`, `internal/humanonly/`: REQ-9.
- `internal/providers/catalog/`: REQ-8.

## Decisions

- **Agreed with the owner**: Codex support is removed, per the revised PRD.
- **Agreed with the owner by approving this ticket**: the named Must-not-trade-off Codex tests go, since what they guard no longer exists. Every other named test stays, and REVIEW.md's P0 rule applies to them.
- **Decided by the ticket writer**: an old Codex file on disk stays, with a line naming it. Deleting a user's config isn't this ticket's call.
- **Decided by the ticket writer**: an old `$CODEX_HOME/hooks.json` still in use makes Codex calls deny (REQ-3), so a leftover Codex session fails closed.
- **Decided by the ticket writer**: REQ-6's rule is a narrow one: keys under an identity's `codex/` folder in an old record are ignored, and any other difference is still a mismatch.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 013` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
