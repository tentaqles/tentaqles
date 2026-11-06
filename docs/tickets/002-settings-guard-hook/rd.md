# 002: The CLI registers the guard hook in the identity's settings

**Status:** parked
**Risk:** High: if the registration or its script is wrong, a Claude Code session runs with no guard, and every tool call passes under any identity
**UI:** none

PRD v2 §6.4 and §5.1: the guard must not depend on the plugin. Today PreToolUse comes from `plugin/hooks/hooks.json`, so removing the plugin removes the guard. This ticket makes `tq settings render` register the hook in each identity's `settings.json`, from a script tq writes under its protected state folder, and generates the identity's instruction file (§6.1 step 1). When in doubt, favour failing closed over convenience.

## Requirements

- **REQ-1**: `tq settings render <ws>` must add one `PreToolUse` entry to `~/.tentaqles/identities/<ws>/claude/settings.json`, with matcher `^(Bash|PowerShell|Read|Edit|Write|MultiEdit|NotebookEdit|Grep|Glob|Agent|WebFetch)$|^mcp__` and timeout 15, whose command runs `$TQ_HOME/hooks/pre-tool-use.sh` through bash.
- **REQ-2**: The render must do REQ-1 for every trusted workspace, with or without `guard: v2`. The rules that run are unchanged by where the hook is registered, so this is not a behaviour change for an unflagged identity.
- **REQ-3**: The render must write `$TQ_HOME/hooks/pre-tool-use.sh` from an asset embedded in the binary, with a mode private to the user.
- **REQ-4**: The script must pass tq's exit status 0 and 2 through unchanged, and turn any other status into a deny with exit 2 and a `BLOCKED [tq/guard-error]` line on stderr.
- **REQ-5**: The script must stop a tq still running after 7 s, and that stop must be a deny. Its total runtime must stay under the 15 s hook timeout.
- **REQ-6**: When no tq binary can be found, the script must deny every tool with exit 2 and a line naming the install step. It has no Python fallback.
- **REQ-7**: The script must resolve tq from PATH then the well-known install folders of today's `plugin/scripts/tq_hook.sh`, and must not take the binary from any environment variable or from the plugin folder.
- **REQ-8**: The integrity record must cover every file under `$TQ_HOME/hooks/`. A changed script must make the identity lose its trust, as a changed plugin hook does today, and `tq hooks verify` must name the file.
- **REQ-9**: With the plugin uninstalled or disabled in the identity's settings, a `.env` read by a Claude Code session must still be denied with `BLOCKED [secrets/tq/env-file-read]`.
- **REQ-10**: A render must keep every hook entry it did not write, in the same order, and running it twice must print `up to date` the second time.
- **REQ-11**: `tq doctor` must report `guard-hook-missing`, as an error with a fix line naming `tq settings render <ws>`, for a trusted workspace whose settings hold no tq entry and whose installed plugin has none either.
- **REQ-12**: `tq run <ws> -- claude` and `tq run . -- claude` must refuse to start, naming the same fix line, in the state of REQ-11. No Claude Code session starts with no hook.
- **REQ-13**: Under `guard: v2`, the render must write a block between `<!-- tq:scope -->` and `<!-- /tq:scope -->` in `<identity>/claude/CLAUDE.md`, holding the text of today's `scopeLine`, and keep the rest of the file byte for byte. With no `guard: v2` it must remove only that block, and it must not create the file.

## Out of scope

- Removing the plugin's own PreToolUse entry: ticket 014. Until then a call runs through both hooks, and a denial can appear twice in the log.
- The session-start preamble, which stays in the plugin with memory.
- Any change to guard rules.
- Codex's hook file: ticket 013.

## Affected areas

- `internal/claudesettings/`: the hook entry, kept apart from user entries
- `internal/cmd/settings.go`, `internal/cmd/scope_render.go`: write the script and the instruction block
- `internal/cmd/run.go`: the refusal of REQ-12
- `internal/doctor/`: `guard-hook-missing`
- `internal/trust/covered.go`: cover `$TQ_HOME/hooks/`
- `internal/hookscript/` (new): the embedded script and its tests
- `docs/CLAUDE-HOOK.md`, `docs/CLAUDE-SETTINGS.md`, `docs/threat-model.md`, `CHANGELOG.md`
- `docs/adr/0010-guard-hook-in-identity-settings.md`: the ADR for this boundary
- `CLAUDE.md`: Must not trade off, naming the new script's tests beside the plugin's

## Do not touch

- `plugin/hooks/hooks.json`, `plugin/scripts/tq_hook.sh`: ticket 014 retires them, and the named plugin tests stay green until then.
- `internal/guard/`, `internal/policy/`: no rule changes.

## Decisions

- **Agreed with the owner**: one flag, `guard: v2`, for the remaining PRD behaviour.
- **Agreed with the owner**: the plugin stays for memory and the graph, but the guard does not depend on it.
- **Decided by the ticket writer**: the hook entry is written for every trusted workspace, not only flagged ones (REQ-2). Otherwise ticket 014 would strip the guard from any unflagged identity.
- **Decided by the ticket writer**: keep a bash wrapper instead of a bare `tq claude-hook` command. Claude Code runs a call whose hook exits with any status other than 2, so a missing binary would otherwise allow every call.
- **Decided by the ticket writer**: the wrapper has no Python fallback. It denies when tq is missing.
- **Decided by the ticket writer**: `WebFetch` joins the matcher, because the guard already judges its URL (`internal/policy/identity.go`) and §6.4 names every tool that takes a URL.
- **Decided by the ticket writer**: a machine with no bash at all is a known gap, recorded in the ADR. The harness runs hook commands through bash on Windows already, and the plugin hook has the same dependency.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 002` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
