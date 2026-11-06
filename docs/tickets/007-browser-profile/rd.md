# 007: One browser profile per identity

**Status:** parked
**Risk:** High: a login page or automation opened in another client's browser profile acts under the wrong account, with that client's cookies
**UI:** none

PRD v2 §3.5 and §7.1. Each `guard: v2` identity is bound to one Chrome or Edge profile. A URL opened by a terminal or an agent, and a browser automation tool, use that profile. Launching another profile is an `identity` breach and is denied.

## Requirements

- **REQ-1**: The manifest must accept `browser:` with `name` of `chrome` or `edge`, and `profile` naming a profile directory, such as `Profile 2`. Any other `name`, or a value `LooksLikeSecret` flags, must fail to load naming the field.
- **REQ-2**: Under `guard: v2` with a `browser:` block, the identity environment must set `BROWSER` to the shim of REQ-3, and leaving the folder must restore it exactly.
- **REQ-3**: `$TQ_HOME/shims/tq-browser` must open its URL argument with the identity's browser and `--profile-directory=<profile>`, using the browser's install path, not the system's default handler.
- **REQ-4**: The identity PATH must hold shims for `xdg-open` and `open` that do what REQ-3 does for an `http` or `https` URL, and pass any other argument to the real program. On Windows a program named `start` is not reachable from cmd.exe or PowerShell, so REQ-6 handles it.
- **REQ-5**: The hook must deny, as `BLOCKED [identity/other-browser-profile]`, a shell command that starts `chrome`, `msedge`, or `google-chrome`, by name or by path, with a `--profile-directory` or `--user-data-dir` other than the identity's. A start with no such flag is denied too.
- **REQ-6**: The hook must deny, with the same rule, `start <url>`, `Start-Process <url>`, `explorer <url>`, `rundll32 url.dll` and `cmd /c start <url>` under an identity with a `browser:` block, and its message must name `tq-browser <url>` as the way to open it.
- **REQ-7**: Under `guard: v2` with a `browser:` block, `tq settings render` must write the Playwright MCP entries it finds in the identity's Claude config with `--user-data-dir` set to the identity's browser folder, keeping a Playwright entry's other arguments.
- **REQ-8**: The hook must deny, with the same rule, a call to `mcp__claude-in-chrome__*` when the identity's browser is not `chrome`.
- **REQ-9**: `docs/COMMANDS.md` must state that Claude in Chrome attaches to a browser already running, so tq can't verify which Chrome profile it uses.
- **REQ-10**: With no `browser:` block, or without `guard: v2`, the environment, PATH and hook decisions must not change.
- **REQ-11**: `tq doctor` must warn `browser-profile-missing`, with a fix line, when the named profile's folder doesn't exist under the browser's user data folder.
- **REQ-12**: Every deny of REQ-5, REQ-6 and REQ-8 must log one line in the denial log with category `identity`.

## Out of scope

- Safari and Opera: the owner chose Chrome and Edge only, per D-06. A manifest naming them fails to load until they are added.
- Creating a browser profile: the browser makes one the first time `--profile-directory` names it. `reach` records the name, in ticket 010.
- Changing the OS default-handler registration: it is machine-wide on Windows and can't be scoped to one session, so §3.5's override is met by REQ-4 and REQ-6 instead.
- Puppeteer as a named case: a Puppeteer script is a program the text filter can't see into (AR2), and when it starts Chrome by name REQ-5 applies.

## Affected areas

- `internal/manifest/`: the `browser:` block
- `internal/envplan/`: `BROWSER`, the shims on the PATH
- `internal/cmd/scope_render.go`, `internal/mcpscope/`: the shims and Playwright entries
- `internal/policy/identity.go`, `internal/policy/policy.go`: the new identity rule and its fixture
- `internal/doctor/`
- `docs/COMMANDS.md`, `docs/CLAUDE-HOOK.md`, `docs/threat-model.md`, `CHANGELOG.md`, `docs/adr/0015-browser-profile.md`
- `CLAUDE.md`: Must not trade off names the rule's test

## Do not touch

- `internal/cmd/claudehook.go`: the rule runs through the existing identity path.
- `internal/providers/catalog/`: ticket 008 owns it.

## Decisions

- **Agreed with the owner**: Chrome and Edge only.
- **Decided by the ticket writer**: `other-browser-profile` is a new match in the existing `identity` category, not a new category (§7.1).
- **Decided by the ticket writer**: a browser started with no profile flag is denied, because Chrome then picks the last-used profile.
- **Decided by the ticket writer**: `--user-data-dir` for Playwright is `$TQ_HOME/identities/<ws>/browser`. Playwright can't attach to a live Chrome profile that is already open, so it gets its own folder.
- **Decided by the ticket writer**: Claude in Chrome stays allowed on a Chrome identity, and is documented as unverifiable (REQ-9), since it can't be launched with a profile.
- **Decided by the ticket writer**: the rule stays quiet for a URL in a commit message, a grep pattern or an echo, using the existing per-shell text masking.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 007` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
