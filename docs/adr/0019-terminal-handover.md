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
tq cannot be sure which one it would end, or that the new one will route, and to say so.

Crossing a trust boundary: this ends a process (`internal/termopen`, `internal/cmd/reach.go`) and writes the shell
profile of the shell reach runs in (`internal/hooks`, through the same install path as `tq hooks install`). The
shell profiles are outside the integrity record, so the block is written only after the typed yes.

## Decision

- **A new terminal opens after a successful new-client reach, in the client's folder.** `termopen.Open` starts it:
  Windows Terminal (`wt -d`) when it is on PATH and the folder has no `;`, which runs the system's default shell
  (tq does not choose it, as the ticket decided). Otherwise, the fallback is PowerShell in a new console:
  `cmd /c start "" powershell -NoLogo`, with the folder only as the working directory, never parsed (REQ-11,
  `TestPlan_HostileFolderNames`). Terminal.app on macOS; the first emulator on Linux.
- **The launcher is watched briefly.** `start` waits up to two seconds for the launcher. One that exits non-zero in that
  time did not open a window, and the error carries its last stderr line (`TestStart_ReportsALauncherThatFailsAtOnce`).
  One that exits 0 handed the window off; one still running is a window.
- **The new terminal starts without tq's identity.** Reach gives the launcher the environment `envplan.Clean` leaves:
  what the tq shell hook set is restored, every catalog provider's config variable is dropped, and `TQ_WS` and
  `TQ_WS_ROOT` are removed (`TestReach_NewTerminalInheritsNoIdentity`).
- **The profile hook is checked and installed after the yes.** Reach collects the shells to hook: every shell
  `tq hooks install --all-detected` would offer (`hooks.Detect`), and the shell reach runs in, read from its parent
  process by name (`termopen.ShellName`). Where a shell's block is missing, the confirmation says so and names the
  profile: `...and trust it; and write the tq activation block into <profile>`. After the yes, reach writes it with
  `hooks.Install`, the same bytes `tq hooks install` writes (so `tq hooks remove` takes it back), and a profile that
  already has the block is left alone. The install runs after the trust is signed, so a reach that fails before that
  writes no profile.
- **Which failures stop the handover (Q2).** The shell reach runs in must take the block, and if it is PowerShell it
  must load the profile: reach probes it with `$PROFILE.CurrentUserCurrentHost` and `Get-ExecutionPolicy`, in a bounded
  run with no profile of its own. The path comes back as base64 of its UTF-8 bytes, so a non-ASCII folder (a João
  profile) is not garbled by the console's code page (`TestCheckPowerShellProbe`). A profile at another path, or a Restricted or AllSigned policy, is the same case as a
  failed write. Then reach prints the reason, opens nothing and ends nothing, and exits 0 (`TestReach_PowerShellProbeFailureOpensNothing`).
  A failure on any other detected shell is a warning naming that shell, and the handover goes on
  (`TestReach_OtherShellFailureWarnsAndHandsOver`).
- **Which new windows route (Q1).** A window in the system's default shell is taken to route (the ticket's decision for
  Windows Terminal). The Windows fallback is PowerShell, and it is taken to route only when its block is in place and
  PowerShell loads it. When it does not, reach prints `Opened a new terminal in <folder>, but tq's shell hook is not
  confirmed to load there, so it is not set up for <name>. Run tq run <name> -- claude in it ...`, prints no login line,
  and keeps the old terminal open (`TestReach_FallbackWithoutTheHookNamesTqRun`). The fallback with a confirmed hook is
  an ordinary handover (`TestReach_FallbackWithTheHookEndsTheOldTerminal`).
- **The old terminal ends only when all of these hold:** the new terminal started; reach's direct parent is readable,
  not a reused pid, and its command line is known; its name is a shell reach knows (`powershell`, `pwsh`, `bash`,
  `zsh`, `fish`, `cmd`); `termopen.Interactive` says that shell's command line starts an interactive session; and the
  parent is not itself a child of a shell. A shell started by a shell (a nested shell, or a Git Bash stub) is not ended
  (`TestReach_KillsOnlyADirectShellParent`, `TestReach_GitBashStubNotEnded`). Only that pid is ended, never an ancestor.
- **Interactive is read word by word, split the way the host hands the line over.** On Windows the line is split as the
  OS does (`CommandLineToArgv`), so a quoted program path with a space, such as `"C:\Program Files\PowerShell\7\pwsh.exe"`,
  is the program and not a script (`TestInteractive`, Windows cases). On Linux and macOS, `internal/humanonly` hands over
  argv joined with plain spaces and no quoting, so the line is split on whitespace only and no shell quoting is re-applied:
  an apostrophe or double quote in a value is a literal character, and a value with a space becomes extra words, which read
  as a script. A doubt (an unclosed quote of either kind, a trailing backslash) keeps the shell open. The word rules are the
  same on every OS and are tested on every OS (`TestInteractive`, the joined-line table). A bare word after the options is
  a script or a command, and makes the shell non-interactive. So do `-c`, `-Command`, `-EncodedCommand` (and its prefixes `-e`, `-ec`, `-enc`), `-File`,
  `-NonInteractive`, `--command`, and `/c` for cmd. Options known to take a value (`-ExecutionPolicy`, `-o`, `--rcfile`)
  consume the next word. An option not on that list leaves its value counted as a script, so every doubt keeps the
  shell open (`TestInteractive`).
- **Keys typed during the installs do not answer the pause.** Before the prompt, reach discards the input the terminal
  has buffered (`FlushInput`: the console on Windows, `TCFLSH` on Linux, `TIOCFLUSH` on macOS; nothing on other systems).
- **The pause comes before the end, and the parent is checked again after it.** Reach prints everything it has to say,
  then `Press Enter to close this terminal.`, and reads one line of stdin; a closed input counts as Enter. Then it reads
  the parent again: a parent whose pid, name or command line changed while the pause waited is not ended
  (`TestReach_ParentChangedAtPauseNotEnded`).
- **The kill exits cleanly.** On Windows the parent is terminated with exit code 0, not 1, so Windows Terminal closes its
  tab (its `closeOnExit` default keeps a tab whose shell exited with an error). Elsewhere the process is killed as before.
- **Otherwise the old terminal stays open, and reach says so.** A parent that may not be ended, an unreadable parent, a
  parent that changed during the pause, or a kill that fails, prints `Close this terminal when you are ready.` and exits 0.
- **A new terminal that cannot start leaves the old one open.** Reach prints
  `Could not open a terminal (<reason>). Open one in <folder> to start working.` and exits 0 (`TestReach_TerminalWontOpen`).
- **`--no-terminal`** skips the open, the pause and the end. It still installs the profile block, since the developer
  will open a terminal by hand, and prints the next step, as before.
- **Nothing opens or ends for an existing client, a declined yes, a bad folder, or an agent session**
  (`TestReach_NoHandoverWithoutANewClient`, `TestReachDoesNotTrustFromAgent`). The human check runs first, so an agent
  never reaches the seams.
- **Seams.** The launcher (`reachOpenTerminal`), the killer (`reachKill`), the PowerShell probe (`reachProbe`) and the
  detected shells (`reachDetectShells`) are variables in `internal/cmd`. `TestMain` replaces them with no-op fakes, and
  `TestMain` replaces `os.Stdin` with a closed pipe, so no test under the cmd suite opens or ends a real terminal, runs
  PowerShell, or waits for a real stdin.

## Consequences

- `tentaqles reach` now changes the developer's terminal: a new window opens, and the one reach ran in may close. This
  is the feature. `--no-terminal` is the way to keep the old behaviour.
- Git Bash: a native program run from bash, directly, in a subshell, or in a pipeline, has a `bash.exe` stub as its
  direct Windows parent, and the interactive bash is the stub's parent (checked on Windows with a probe that read the
  parent and grandparent images). A developer who runs `tentaqles reach` from Git Bash therefore gets the `Close this
  terminal` line and keeps the terminal. The handover ends terminals started by Windows Terminal, a console, or a
  terminal emulator, not bash.
- The shells reach hooks are every shell tq detects. On Windows that includes Windows PowerShell whenever its executable
  is on PATH, and Git Bash's `bash` when it is on PATH, so reach may create a profile the developer did not have. This is
  what `tq hooks install --all-detected` already does; the decision was taken so the new terminal routes whichever shell
  it starts.
- A PowerShell whose profile is not at the path `$PROFILE` names (a Documents folder redirected by OneDrive, say) gets
  the block in the wrong file, and reach then refuses the handover with the reason. tq does not yet write to a redirected
  `$PROFILE`: the fix is in `hooks.DefaultProfiles`, a change of its own.
- The Windows fallback opens PowerShell, not cmd.exe, so a developer with no Windows Terminal gets a window that routes
  (with a working policy and profile). A window whose hook is not confirmed keeps the old terminal open and says `tq run`.
- The shell is killed, not asked to exit, so anything it has not yet written (shell history, say) may be lost. The
  developer has read the output and pressed Enter first, and the parent is the only process ended.

## Not covered

- A real window opening and closing, the Windows Terminal close on exit code 0, and the PowerShell probe on a real
  Restricted or redirected machine are human checks (eval U-04, and the owner's runs). The tests use the seams.
- Windows Terminal profiles and other GUI integration: out of scope (rd.md).
- The new terminal's banner and claude starting by itself: out of scope (rd.md).
