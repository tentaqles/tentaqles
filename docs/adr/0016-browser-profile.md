# ADR-0016: The right browser on the right profile

Status: accepted
Date: 2026-11-06
Cites: `docs/IDEATION.md` (the spec), ADR-0004 (signed record), ADR-0005 (self-protection), ADR-0007 (identity rules), ADR-0008 (identity PATH), ADR-0012 (`browsers:` in the manifest)

## Context

IDEATION: whenever a terminal or an agent interacts with a browser, it opens the correct browser on the
correct profile. `tentaqles reach` records a `browsers:` list (Chrome or Edge, and a profile folder each),
and nothing read it. A login page or an automation opened in another client's profile acts under the wrong
account, with that client's cookies. A browser started with no profile flag opens the profile used last, and
the OS's default handler (`start <url>`, `xdg-open`, `open`) opens whatever the system chose.

Crossing a trust boundary: this changes `internal/policy/`, `internal/trust/` (what the record covers) and
the PATH and environment of an identity (`internal/envplan/`), so it needs this ADR.

## Decision

- **`tentaqles open <url>`** opens an `http` or `https` URL in the client's first listed browser (or the one
  `--browser` names), started from its install path with `--profile-directory=<profile>`. It never uses the
  default handler, never runs a shell (the program and each argument are separate argv elements), refuses any
  other scheme, a value starting with `-`, a URL with a space, quote or control character, a neutral folder, a
  client with no `browsers:` and a listed browser that is not installed, and opens nothing in each case. It
  writes nothing under the tq home, so it is not human-only.
- **Shims.** For a client with `browsers:`, `tq settings render` (and `tq allow`) writes three programs to
  `$TQ_HOME/shims/<ws>/`: `tq-open`, `xdg-open` and `open`. `tq-open` is a copy of the tq binary and the others
  are links to it. The binary answers as a shim when it runs from such a folder (`cmd.ShimMain`, beside
  `StubMain`): `tq-open` opens its one URL, `xdg-open` and `open` do the same for every argument that starts with `http://` or `https://` (opened in the
  client's profile, or refused with nothing opened, never passed on; `open -a "Google Chrome"` and
  `open -a "Microsoft Edge"` go to the client's profile, `open -a <another app>` passes through) and
  run the real program, found on PATH outside the shims folder, for anything else. On every OS they are
  programs, not batch files: cmd.exe re-parses a batch file's arguments, and a URL with `&` in it would run the
  rest as a command. The shim resolves the client from its own folder name, and refuses unless that client's
  integrity record verifies.
- **Environment.** The identity environment sets `BROWSER` to `tq-open` (forward slashes on Windows, in double
  quotes if the path has a space) and puts the shims folder first on the identity PATH. Both are recorded in
  `__TQ_STATE` like every other change, so leaving the folder restores them byte for byte; a lost state removes
  a `BROWSER` that names a shim and the shims folder on PATH, and leaves the user's own `BROWSER`. With no
  `browsers:` nothing changes.
- **Integrity record.** The record already covered every file under `$TQ_HOME/shims/`. A shared folder would
  make writing client A's shims change client B's record, so the folder is per identity and a record covers
  its own folder (`shims/<name>/`) and any file directly under `shims/`, not another identity's folder. A
  render that changes the shims signs the record again, as it does for `settings.json`.
- **Hook.** A new match of the `identity` category, `other-browser-profile`, for a client with `browsers:`:
  - a shell command whose program is `chrome`, `msedge`, `google-chrome` (and the `-stable` and `microsoft-edge`
    spellings), by name or path, is denied unless the browser is listed and the command names the client's
    profile with `--profile-directory`, or a `--user-data-dir` under the client's own folder; with neither
    flag it is denied, because Chrome then picks the last-used profile, except `--version`, `--help` and
    `--headless`, which choose no profile (the owner's decision, round 1; a headless start with another
    profile is still denied); behind `start` and `Start-Process`
    the same check reads the arguments;
  - `start <url>`, `Start-Process <url>`, `explorer <url>`, `rundll32 url.dll` and `cmd /c start <url>` are
    denied, and the message names `tentaqles open <url>`;
  - an `mcp__claude-in-chrome__*` call is denied when Chrome is not one of the client's browsers.
  Quoted text (a commit message, a grep pattern, an `echo`) stays text, through the same tokenizer and masking
  the rest of the category uses. Like the rest of the hook it is a text filter on literal spellings (AR2).
  Two spellings of the plain forms are not denied, and no code or fixture covers them (round 2 ruling, AR2):
  `start "Docs" https://x`, where the quotes are read away and `Docs` looks like the program to start, and
  `start -FilePath msedge`, where cmd's `start` does not read the `-FilePath` flag of PowerShell.
- `--headless` with no profile flag is allowed by the owner's decision (round 1). Which user data folder a
  headless start uses is unconfirmed: if it is the default one, a headless page reads that profile's cookies.
  Accepted knowingly by the owner (round 2 ruling); `docs/COMMANDS.md` says the same.
- **Playwright.** The Playwright MCP server cannot attach to a profile that is already open, so
  `tq settings render` sets `--user-data-dir` to `$TQ_HOME/identities/<ws>/browser` on its entries in the
  client's `.claude.json` (user and project scope), replacing an existing value and keeping the other
  arguments, editing only that entry's `args` array.
- **Claude in Chrome** attaches to a browser that is already running, in whatever profile it has: tq cannot
  verify it. It stays allowed for a client that lists Chrome, and `docs/COMMANDS.md` says so.
- **Doctor** warns `browser-profile-missing` when a named profile has no folder in the browser's user data
  folder, and `browser-shims-missing` when the shims are not written.

## Consequences

- The shims are copies of the tq binary, so the record covers about 7 MB under three names. A
  verification reads a file of 1 MiB or more once per inode (`os.SameFile`, a hard link) within one call
  (`internal/trust/hash.go`); no size, time or earlier result decides anything, so a same-size, same-time
  rewrite is read and a separate copy is read in full. Measured at one core: 18.7 ms for three copies, 7.0 ms
  for three links. A tq upgrade does not touch them: they keep running the old binary until
  `tq settings render <client>` (or `tq hooks verify --accept`) writes the new copy and signs it.
- A browser started by a script (Puppeteer, a Python `webbrowser` call that does not read `BROWSER`) is
  something the text filter cannot see into (AR2); starting Chrome by name still meets the rule.
- The OS default-handler registration is machine-wide on Windows and is not changed.
- Safari and Opera are not supported; a manifest naming them fails to load.
