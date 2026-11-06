# 005: The identity terminal opens with the banner, and cmd.exe routes itself

**Status:** ready
**Risk:** High: a broken profile hook or AutoRun value breaks every terminal on the machine, and a banner on a scripted shell corrupts another program's output
**UI:** none

PRD v2 §3.1, §4.1 and §9.4. The developer always sees which identity and guard state a terminal is in. cmd.exe, which has no profile hook, gets routed too. Other programs running `cmd /c` or a non-interactive shell must see no difference, so favour silence over showing a banner when in doubt.

## Requirements

- **REQ-1**: The banner must ship as one text asset, `internal/banner/banner.txt`, holding the UTF-8 art of PRD §4.1 and the colour scheme, and `internal/banner/banner-ascii.txt`, holding the fallback art, so that replacing those files changes it.
- **REQ-2**: `tentaqles banner` must print the art, then the line `  tentaqles <version>  ·  identity: <name>  ·  guard: on`, then `  <working directory>`.
- **REQ-3**: On a terminal that reports no UTF-8, it must print the fallback art and use ` | ` in place of `  ·  `, with no non-ASCII byte in its output.
- **REQ-4**: `TQ_NO_BANNER=1` or `--no-banner` must print nothing. A stdout that is not a terminal must print nothing. `NO_COLOR` set must print no escape sequence.
- **REQ-5**: The art must be cyan, the status line in the default colour, and `guard: off` plus the warning line of ticket 006 yellow.
- **REQ-6**: A new interactive terminal opened in a `guard: v2` identity root must print the banner as its first output, in PowerShell, pwsh, Git Bash, bash, zsh, fish and cmd.exe.
- **REQ-7**: A terminal that enters a `guard: v2` identity by `cd` must print only the two status lines, once per entry, and not the art.
- **REQ-8**: An identity without `guard: v2`, and a neutral folder, must print no banner and no status line.
- **REQ-9**: `tq hooks install` must, on Windows after the human's confirmation, set `HKCU\Software\Microsoft\Command Processor\AutoRun` to run a script under `$TQ_HOME/shell/`, keeping any existing value by chaining it.
- **REQ-10**: Under `cmd /c` and `cmd /k` run by another program with a command, the AutoRun script must exit immediately, change nothing and print nothing, in under 50 ms of its own work.
- **REQ-11**: A cmd.exe opened by hand in an identity root must have the identity environment and the banner.
- **REQ-12**: `tq hooks remove` must restore the AutoRun value byte for byte, type included, or delete it if there was none.
- **REQ-13**: `tq hooks status` must report the AutoRun state, and `tq migrate` must clear only the legacy shim value and leave tq's new one alone.
- **REQ-14**: `docs/COMMANDS.md` must state the cmd.exe limit: its identity is decided when it starts, and `cd` to another client does not switch it.

## Out of scope

- `tentaqles reach` and `edit` printing the banner: ticket 010 calls this code.
- Raw sessions: ticket 006.
- A Windows Terminal profile, an Explorer entry, or anything else with a GUI. PRD §10 forbids them.
- A prompt prefix or window title (old FR-25). The PRD now asks for the banner and status line only.

## Affected areas

- `internal/banner/` (new): assets, detection, rendering
- `internal/cmd/banner.go` (new), `internal/cmd/root.go`: the command
- `internal/shell/hooks/*.tmpl`, `internal/shell/testdata/*`: print it
- `internal/hooks/`: AutoRun install, remove and status on Windows
- `internal/cmd/hooks.go`: wiring, and the confirmation text
- `internal/migrate/cmdstep.go`: tell tq's value from the legacy one
- `internal/doctor/`: AutoRun in the hook checks
- `docs/COMMANDS.md`, `docs/MIGRATE.md`, `CHANGELOG.md`, `docs/adr/0013-identity-terminal-and-cmd-autorun.md`

## Do not touch

- `internal/envplan/`: the identity environment does not change here.
- `internal/policy/`: no new rule.

## Decisions

- **Agreed with the owner**: AutoRun, interactive only, installed by `tq hooks install` with confirmation, restored by `tq hooks remove`.
- **Agreed with the owner**: the cmd.exe identity is decided at start, and `cd` does not switch it.
- **Decided by the ticket writer**: PRD §4.1 replaces D-01 ("banner once, at onboarding"). The old PRD's FR-29 gets updated.
- **Decided by the ticket writer**: colours are cyan art and yellow warnings, kept in the asset. The PRD left colour open and asked for one proposal.
- **Decided by the ticket writer**: `cd` into an identity prints status lines only, since a 7-line banner on every `cd` would push work off the screen. The PRD only says first output of a terminal.
- **Decided by the ticket writer**: the terminal's UTF-8 check uses the console's code page on Windows and `LC_ALL`, `LC_CTYPE` and `LANG` elsewhere.
- **Decided by the ticket writer**: the banner reads `tentaqles <version>` from the binary's real version and not the `0.1.0` in the PRD's sample.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 005` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
