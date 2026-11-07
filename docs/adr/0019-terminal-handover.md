# ADR-0019: `tentaqles reach` hands the developer a terminal in the client, and ends the old one

Status: accepted
Date: 2026-11-07
Cites: `docs/IDEATION.md` (the spec: "starts a new terminal session with the correct identity and closes the terminal where the setup was started"; "favour leaving a terminal open over closing one tq can't identify"), ADR-0012 (`tentaqles reach`), ADR-0008 (identity PATH), ADR-0003 (the human-only check and its process-tree walk), ADR-0018 (the plugin install that reach does before the handover)

## Context

Until now `tentaqles reach` made a client and printed what to do next: open a new terminal in the folder. The
developer then had to open a terminal, navigate, and start claude. IDEATION has reach start the new terminal with the
client's identity and close the terminal reach was run from.

Ending a terminal is the one action here that can lose work: a shell with unsaved output, or a shell tq does not
recognise, is a developer's work and not tq's to close. A new terminal that does not route into the client runs under
no identity, which is the failure the guard exists to stop. So the handover is built to leave a terminal open whenever
tq cannot be sure which one it would end, and to say so.

Crossing a trust boundary: this ends a process (`internal/termopen`, `internal/cmd/reach.go`) and writes the shell
profile of the shell reach runs in (`internal/hooks`, through the same install path as `tq hooks install`). The
shell profiles are outside the integrity record, so the block is written only after the typed yes.

## Decision

- **A new terminal opens after a successful new-client reach.** `termopen.Open` starts a window in the client's folder:
  Windows Terminal (`wt -d`) when it is on PATH and the folder has no `;`, else `cmd /c start`, which passes the folder
  only as the working directory; Terminal.app; the first emulator on Linux. The folder is one argument or the working
  directory, never part of a command line a shell parses (REQ-11, `TestPlan_HostileFolderNames`). The new terminal
  uses the system's default shell, as `termopen` did before.
- **The profile hook is checked and installed after the yes.** Reach collects the shells to hook: every shell
  `tq hooks install --all-detected` would offer (`hooks.Detect`), and the shell reach runs in, read from its parent
  process by name (`termopen.ShellName`). Where a shell's block is missing, the confirmation says so and names the
  profile: `...and trust it; and write the tq activation block into <profile>`. After the yes, reach writes it with
  `hooks.Install`, the same bytes `tq hooks install` writes (so `tq hooks remove` takes it back), and a profile that
  already has the block is left alone. The install runs after the trust is signed, so a reach that fails before that
  writes no profile.
- **The old terminal ends only when all of these hold:** the new terminal started; reach's direct parent (read with
  `humanonly.ParentOf`, the same walk the human check uses, with no change to the check) is readable, not a reused
  pid, and its command line is known; its name is a shell reach knows (`powershell`, `pwsh`, `bash`, `zsh`, `fish`,
  `cmd`); and `termopen.Interactive` says that shell's command line starts an interactive session (no `-c`,
  `-Command`, `-EncodedCommand`, `-File`, `-NonInteractive`, `/c`, `--command`). Only that pid is ended, never an
  ancestor: a shell run under `python` under `pwsh` ends nothing (`TestReach_KillsOnlyADirectShellParent`).
- **The pause comes before the end.** Reach prints everything it has to say, then `Press Enter to close this terminal.`,
  and reads one line of stdin. A closed input counts as Enter. Only then is the parent ended.
- **Otherwise the old terminal stays open, and reach says so.** A parent that is not an endable shell, an unreadable
  parent, or a kill that fails, prints `Close this terminal when you are ready.` and exits 0.
- **A new terminal that cannot start leaves the old one open.** Reach prints
  `Could not open a terminal (<reason>). Open one in <folder> to start working.` and exits 0.
- **A profile that cannot take the block opens and ends nothing.** A `hooks.Install` failure (a corrupt block, an
  unreadable profile, a folder that cannot be made) is printed. Reach does not open a terminal, since it would not
  route into the client, and does not end the old one. The client is still made and trusted, and reach exits 0.
- **`--no-terminal`** skips the open, the pause and the end. It still installs the profile block, since the developer
  will open a terminal by hand, and prints the next step, as before.
- **Nothing opens or ends for an existing client, a declined yes, a bad folder, or an agent session**
  (`TestReach_NoHandoverWithoutANewClient`, `TestReachDoesNotTrustFromAgent`). The human check runs first, so an agent
  never reaches the seams.
- **Seams.** The launcher (`reachOpenTerminal`), the killer (`reachKill`) and the detected shells (`reachDetectShells`)
  are variables in `internal/cmd`. `TestMain` replaces them with no-op fakes, so no test under the cmd suite opens or ends
  a real terminal, and `isolateHandover` replaces them with recording fakes for the handover tests.

## Consequences

- `tentaqles reach` now changes the developer's terminal: a new window opens, and the one reach ran in may close. This
  is the feature. `--no-terminal` is the way to keep the old behaviour.
- The shells reach hooks are every shell tq detects. On Windows that includes Windows PowerShell whenever its executable
  is on PATH, and Git Bash's `bash` when it is on PATH, so reach may create a profile the developer did not have. This is
  what `tq hooks install --all-detected` already does; the decision was taken so the new terminal routes whichever shell
  it starts.
- The cmd.exe fallback of `termopen` (no `wt`) starts `cmd.exe`, which has no profile hook (IDEATION puts cmd.exe under
  `tq run`). A developer on that path gets a new terminal that does not route into the client until they run `tq run`
  or open PowerShell. The ticket keeps the system default shell, so this is a known gap, not a silent one: the reach
  output names the folder and the next step.
- The shell is killed, not asked to exit, so anything it has not yet written (shell history, say) may be lost. The
  developer has read the output and pressed Enter first, and the parent is the only process ended.
- A script named on a shell's command line without a flag is not caught by `termopen.Interactive`. Reach still needs a
  terminal on its own stdin, so the only case it misses is a shell that runs a script and is ended while the script is
  still running reach.

## Not covered

- A real window opening and closing is a human check (eval U-04). The tests use the seams.
- Windows Terminal profiles and other GUI integration: out of scope (rd.md).
- The new terminal's banner and claude starting by itself: out of scope (rd.md).
