# 017: The right browser on the right profile

**Status:** landed
**Risk:** High: a login page or automation opened in another client's browser profile acts under the wrong account, with that client's cookies
**UI:** none

IDEATION: "Isolated Chrome and/or Edge profiles, so whenever a terminal or an agent interacts with a browser it opens the correct browser on the correct profile." `docs/IDEATION.md` is the spec. `tentaqles reach` already records a `browsers:` list in the manifest (`internal/manifest/browser.go`). Nothing reads it yet.

## Requirements

- **REQ-1**: `tentaqles open <url>` must open an `http` or `https` URL in the client's first listed browser, with `--profile-directory=<profile>`, using the browser's install path and not the system's default handler. `--browser edge` must pick another listed browser.
- **REQ-2**: `tentaqles open` must refuse any other URL scheme, a value starting with `-`, and a neutral folder, opening nothing.
- **REQ-3**: A client with no `browsers:` must get `no browser is set for this client`, exit 1.
- **REQ-4**: A listed browser that isn't installed must get `<browser> is not installed`, exit 1, opening nothing else.
- **REQ-5**: The client's environment must set `BROWSER` to a program under `$TQ_HOME/shims/` that does what REQ-1 does, and leaving the folder must restore `BROWSER` exactly.
- **REQ-6**: The client's PATH must hold `xdg-open` and `open` programs from that folder that do what REQ-1 does for an `http` or `https` URL, and pass anything else to the real program.
- **REQ-7**: `tq settings render` must write those programs, and the integrity record must cover them.
- **REQ-8**: The hook must deny, as `BLOCKED [identity/other-browser-profile]`, a shell command that starts `chrome`, `msedge` or `google-chrome`, by name or path, with a `--profile-directory` or `--user-data-dir` other than the client's, or with neither flag.
- **REQ-9**: The hook must deny, with the same rule, `start <url>`, `Start-Process <url>`, `explorer <url>`, `rundll32 url.dll` and `cmd /c start <url>` for a client with `browsers:`, and the message must name `tentaqles open <url>`.
- **REQ-10**: `tq settings render` must set `--user-data-dir` under `$TQ_HOME/identities/<ws>/browser` on the Playwright MCP entries in the client's Claude config, keeping their other arguments.
- **REQ-11**: The hook must deny, with the same rule, a `mcp__claude-in-chrome__*` call when Chrome is not one of the client's browsers.
- **REQ-12**: `docs/COMMANDS.md` must state that Claude in Chrome attaches to a browser already running, so tq can't verify its profile.
- **REQ-13**: With no `browsers:`, the environment, PATH and hook decisions must not change, except that REQ-3 applies to `tentaqles open`.
- **REQ-14**: `tq doctor` must warn `browser-profile-missing`, with a fix line, when a named profile's folder doesn't exist in that browser's user data folder.
- **REQ-15**: Every deny of this rule must log one line with category `identity`.

## Out of scope

- Safari and Opera. A manifest naming them already fails to load.
- Creating a profile: the browser does that the first time `--profile-directory` names it.
- Changing the OS default-handler registration, which is machine-wide on Windows.
- Puppeteer as a named case: a script is something the text filter can't see into (AR2), and starting Chrome by name still meets REQ-8.

## Affected areas

- `internal/browser/` (new): find the browser, build its command
- `internal/cmd/open.go` (new), `internal/cmd/root.go`, `cmd/tq/main.go`, `cmd/tentaqles/main.go`: the command, and the shim's entry beside `StubMain`
- `internal/envplan/`: `BROWSER`, and the shims folder on the client's PATH
- `internal/cmd/scope_render.go`, `internal/mcpscope/`: the shims, and Playwright entries
- `internal/policy/identity.go`, `internal/policy/policy.go`: the rule and its fixture
- `internal/doctor/`
- `docs/COMMANDS.md`, `docs/CLAUDE-HOOK.md`, `docs/threat-model.md`, `CHANGELOG.md`, `docs/adr/0016-browser-profile.md`
- `CLAUDE.md`: Must not trade off names the rule's test

## Do not touch

- `internal/cmd/claudehook.go`: the rule runs through the existing identity path.
- `internal/cmd/reach.go`: reach already records the browsers.

## Decisions

- **Agreed with the owner**: Chrome and Edge only.
- **Decided by the ticket writer**: `other-browser-profile` is a new match in the `identity` category, not a new category.
- **Decided by the ticket writer**: a browser started with no profile flag is denied, because Chrome then picks the last-used profile.
- **Decided by the ticket writer**: with two browsers listed, the first is the default, and `BROWSER` uses it.
- **Decided by the ticket writer**: Playwright gets its own folder under the identity, because it can't attach to a live profile that's already open.
- **Decided by the ticket writer**: Claude in Chrome stays allowed on a Chrome client and is documented as unverifiable.
- **Decided by the ticket writer**: on Windows the shims are programs, like the PATH mask's stubs, not `.cmd` files, because cmd.exe re-parses a batch file's arguments.
- **Decided by the ticket writer**: a URL in a commit message, a grep pattern or an echo stays allowed, through the existing per-shell text masking.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 017` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
