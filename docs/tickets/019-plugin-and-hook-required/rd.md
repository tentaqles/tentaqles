# 019: `reach` installs the plugin, and Claude won't start without the guard

**Status:** landed
**Risk:** High: a Claude Code session started with no hook has no guard at all, under any identity
**UI:** none

IDEATION: "the tentaqles plugin is already installed for that harness, or the developer installs it", and "a hook blocks it". The hook comes only from the plugin, so a client without it has no guard. `docs/IDEATION.md` is the spec. This favours never starting an unguarded session, with a clear way through.

## Requirements

- **REQ-1**: `tentaqles reach`, after the trust is signed, must install the tentaqles plugin into the client's Claude config, by running `claude plugin marketplace add tentaqles/tentaqles` and `claude plugin install tentaqles@tentaqles` with that client's `CLAUDE_CONFIG_DIR`, with a 2-minute limit, then re-sign the record.
- **REQ-2**: When the plugin can't be installed, or `claude` isn't found, reach must print the reason and the exact line for the developer to run, and exit 0 with the client still set up.
- **REQ-3**: `--no-plugin` must skip the install.
- **REQ-4**: `tq run <ws> -- claude` and `tq run . -- claude` must refuse to start a session, exit non-zero, and print the fix line, when the client's Claude config has no enabled tentaqles plugin with its `PreToolUse` hook.
- **REQ-5**: The fix line must be `tq run <ws> -- claude plugin install tentaqles@tentaqles`.
- **REQ-6**: `claude` subcommands (the `claudeSubcommands` list), `--help` and `--version` must not be refused, so the fix line itself runs.
- **REQ-7**: `tq doctor` must report `guard-hook-missing` as an error with the same fix line in that state.
- **REQ-8**: A plugin disabled by `enabledPlugins` false, or settings with `disableAllHooks` true, must count as no hook. The fix line for `disableAllHooks` must name the setting.
- **REQ-9**: Other commands through `tq run`, such as `tq run acme -- gh pr list`, must not be refused for this reason.
- **REQ-10**: A neutral folder's `claude` must be unchanged.
- **REQ-11**: The hook must keep denying a write to the installed plugin, the settings and the `installed_plugins.json` file.

## Out of scope

- Moving the hook out of the plugin into settings: the plugin holds it, as IDEATION describes.
- Refusing a `claude` that a human runs by its absolute path: that's outside what tq launches (AR1).
- The release: the marketplace serves the last pinned release until a new one is cut.

## Affected areas

- `internal/cmd/run.go`, `internal/cmd/reach.go`: the refusal, and the step in reach
- `internal/trust/covered.go`: read which plugin is loaded, as it already does
- `internal/doctor/`: `guard-hook-missing`
- `docs/COMMANDS.md`, `docs/CLAUDE-HOOK.md`, `docs/threat-model.md`, `CHANGELOG.md`, `docs/adr/0018-no-session-without-the-hook.md`
- `CLAUDE.md`: Must not trade off names the refusal's test

## Do not touch

- `plugin/`: the hook itself doesn't change.
- `internal/policy/`: no rule changes.

## Decisions

- **Decided by the ticket writer**: the check reads the plugin's `hooks/hooks.json` through `trust.PluginRoot`, the same way the integrity record finds the plugin, so the two can't disagree.
- **Decided by the ticket writer**: a failed plugin install doesn't fail reach, since IDEATION allows the developer to install it. The refusal is what keeps a session from running unguarded in the meantime.
- **Decided by the ticket writer**: the refusal covers only `claude` through tq. A real `claude` not launched by tq, in a terminal without the hook block, is outside it, and the docs say so.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 019` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
