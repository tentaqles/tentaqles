# 020: `reach` opens the client's terminal and closes the old one

**Status:** landed
**Risk:** High: closing the wrong terminal loses a developer's work, and a new terminal that doesn't route into the client runs under no identity
**UI:** none

IDEATION: reach "starts a new terminal session with the correct identity and closes the terminal where the setup was started." `docs/IDEATION.md` is the spec. Today reach only prints what to do next. Favour leaving a terminal open over closing one tq can't identify.

## Requirements

- **REQ-1**: After a successful reach, tq must open a new terminal in the client's folder, using `internal/termopen`.
- **REQ-2**: Reach must check the profile hook for the shell it runs in. When it's missing, reach must install it, after the same typed `yes`, and the confirmation text must say so.
- **REQ-3**: In the new terminal, `CLAUDE_CONFIG_DIR` and the identity PATH must be the client's, through the profile hook.
- **REQ-4**: After the new terminal started, tq must end the terminal that ran reach, only when reach's direct parent process is a shell it knows (`powershell`, `pwsh`, `bash`, `zsh`, `fish`, `cmd`), and that shell is interactive.
- **REQ-5**: When the parent isn't such a shell, or ending it fails, reach must print `Close this terminal when you are ready.` and exit 0.
- **REQ-6**: When the new terminal can't start, the old one must stay open, and reach must print `Could not open a terminal (<reason>). Open one in <folder> to start working.`, and exit 0.
- **REQ-7**: `--no-terminal` must skip both opening and closing, and print the line that says what to do next.
- **REQ-8**: Reach on an existing client must neither open nor close a terminal.
- **REQ-9**: A failed reach, including a declined `yes`, must neither open nor close a terminal.
- **REQ-10**: Before ending the terminal, reach must have printed everything it had to say, including any install failure, and it must pause so the developer can read it: `Press Enter to close this terminal.`
- **REQ-11**: A folder name holding `&`, `;`, `%` or quotes must reach the new terminal as one value, with no shell parsing it.

## Out of scope

- Starting `claude` by itself: IDEATION has the developer start it.
- cmd.exe's AutoRun: IDEATION's terminals are PowerShell, pwsh and Git Bash, and `tq run` covers cmd.exe.
- Windows Terminal profiles or other GUI integration.
- The new terminal's banner: IDEATION asks for it on `reach`.

## Affected areas

- `internal/cmd/reach.go`: the step
- `internal/termopen/`: end the parent shell
- `internal/humanonly/`: reuse `ParentOf` to read the parent, with no change to the check
- `internal/hooks/`: reuse the install path
- `docs/COMMANDS.md`, `README.md`, `CHANGELOG.md`, `docs/adr/0019-terminal-handover.md`

## Do not touch

- `internal/policy/`, `internal/guard/`: no rule changes.

## Decisions

- **Decided by the ticket writer**: the pause in REQ-10, since an instantly closed window hides any failure. Enter closes it, and a closed input counts as Enter.
- **Decided by the ticket writer**: on Windows Terminal or VS Code, ending the shell closes that tab or pane, not the whole app.
- **Decided by the ticket writer**: only reach's direct parent is ever ended, never an ancestor.
- **Decided by the ticket writer**: the new terminal uses the system's default shell, as `termopen` does today, and the profile hook is installed for the shell reach runs in. If those differ, the new terminal may not route, so REQ-2 installs the hook for every shell `tq hooks install` detects.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 020` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
