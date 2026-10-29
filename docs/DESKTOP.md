# Tentaqles Setup (desktop app)

A small Wails desktop app that wraps `tq setup` in a guided wizard: pick a
base folder, add companies and services, check tool availability, preview
the plan, apply it, and get the login commands to run. It shares the same
`internal/`/`pkg/setupapi` logic as the `tq` CLI — nothing it does is
different from running `tq` by hand, it just walks you through it.

## Relationship to `tq setup`

The app is a GUI front end for the same setup facade the CLI uses
(`pkg/setupapi`). Everything the wizard does — validating a plan, previewing
changes, applying them, running doctor — is exactly what `tq setup` and
`tq doctor` do from the terminal. Use whichever you prefer; they read/write
the same `tq` state and are safe to mix.

On first run, if `tq` isn't found on your `PATH`, the app shows a banner to
install it. If a `tq` binary is bundled with the app, clicking "Install tq"
copies it into `tq`'s per-user install directory. Otherwise the banner falls
back to the published one-liner installers (`curl ... | sh` / `irm ... |
iex`).

The one-liners fetch `installers/install.sh` / `installers/install.ps1`
straight from this repository's `main` branch and download the matching
`tq` archive from the latest GitHub release. Both need the repository to be
public (or a `GITHUB_TOKEN`-authenticated client) — while it is private,
use the bundled binary or a downloaded release archive instead.

### How `tq` is bundled

Each installer ships the matching `tq` binary and puts it where the app
looks for it — next to the running executable, or under `$APPDIR/usr/bin`
on Linux:

| Platform | Installer            | Bundled `tq` lands in                   |
| -------- | -------------------- | --------------------------------------- |
| Windows  | NSIS `.exe` (amd64)  | `$INSTDIR\tq.exe`, beside the app        |
| macOS    | `.dmg` (native arch) | `tentaqles-setup.app/Contents/MacOS/tq` |
| Linux    | `.AppImage` (amd64)  | `usr/bin/tq` inside the AppDir          |

The Windows and Linux installers are **amd64 only**. The macOS `.dmg` is
built for the release runner's **native architecture** (`arm64` on Apple
silicon runners, `amd64` otherwise), and the bundled `tq` is downloaded to
match.

If a build is produced without staging a `tq` binary into
`desktop/build/bin/`, the app still works — the banner simply falls back to
the one-liner installers.

## Services step

For each company the Services step can add, each one optional:

- a **tracker** for the hub's board sync: provider (GitHub, Azure DevOps, Jira, Linear,
  Asana), the scope fields that provider reads (for example organization, project and
  team for Azure DevOps; site and project key for Jira; team for Linear), whose items to
  mirror (assigned to me, or anyone), and how to sign in: the tracker CLI's own login, a
  token in the OS keychain (`tq secret set`), a token through `tq dotenv` (you name the
  variable), or the agent's MCP connector (no background sync). Lane overrides map a
  tracker status to a hub lane; without them statuses map by category;
- **chat** (Slack, Microsoft Teams or none) and **email** (Gmail, Outlook or none), both
  reached by the agent through MCP connectors.

These are written into the company's `.tentaqles.yaml` as the `tracker`, `chat` and
`email` blocks (see [PROVIDERS.md](PROVIDERS.md#company-services-in-the-manifest)), names
only. A token-shaped value in any field is refused, naming the field and never the value.
Logins and tokens are set up afterwards with `tq login`, `tq secret set` or the variable.

## Board tab

The Board tab lists the registered workspaces and the hubs that are running.

- A workspace already served by a running hub opens that hub. Otherwise the app starts
  `tq hub serve --workspace <ws> --port <free loopback port>` as its own child process (no
  console window), waits up to 20 s for it to answer, then navigates the app window itself
  to the board. There is no iframe: the hub's CSP keeps `frame-ancestors 'none'`, and only
  loopback `http` hub URLs are allowed.
- If something takes the chosen port before the child binds it, the start is retried once
  on a new port. When a start fails, the error shows the tail of the child's output.
- A desktop-started hub sends its own desktop notifications (the hub's notify setting).
- The child does not inherit `CLAUDECODE`, `CLAUDE_PLUGIN_ROOT`, `TENTAQLES_HEADLESS` or
  `TQ_HUB_TASK`, so it runs as a human-started hub even when the app was launched from a
  Claude Code terminal.
- When the app closes, it stops the hubs it started; hubs started elsewhere keep running.
  The stop is the hub's own clean shutdown (it drains HTTP, stops its runners and their
  agents, and removes its serve record), triggered by closing the hub's stdin, plus SIGINT
  on macOS and Linux. If the hub has not exited after 10 s, its whole process tree is
  killed, run agents included (a Job Object on Windows, the process group on macOS and
  Linux). On Windows the Job Object also ends the tree if the app crashes.
- **View > Setup** (Ctrl/Cmd+1) brings the wizard back after the window went to a board.
  It reloads the wizard, so unapplied wizard state is reset. On the wizard itself it does
  nothing.

## Guard tab

The Guard tab shows and changes each workspace's guard mode (see `CLAUDE-HOOK.md`,
"Guard modes"). It calls the same code as `tq mode`.

- A workspace picker and a layer picker (the workspace, one of its projects, or the
  global `guard.yaml`).
- A preset control: `default`, `relaxed`, `strict`, `custom`.
- A category grid with a four-way toggle per category (`skip`, `ask`, `deny`, `rule`).
  Toggling a preset turns it into `custom` with that preset as the base. The locked rows
  (`identity`, `secrets`, `self-protection`, `privacy`) are greyed with a lock icon and a
  tooltip naming the group; they cannot be changed.
- The rule overrides (`rules_override`) with a Clear button.
- A ceiling banner when the layers are looser than the grant, with a "Grant relaxed for
  acme" (or "Grant custom …") button and a confirm. Revoke removes the grant.
- The 24 h counts of confirms and blocks the mode skipped or lowered.

The Go side is `desktop/app_guard.go` over `internal/setup/guard.go`; the frontend is
`Guard.tsx`, with its toggle logic in `guardview.ts` (tests in `guardview.test.ts`; not
`guard.ts`, which would clash with `Guard.tsx` on a case-insensitive file system).

Every change re-trusts the manifest like `tq mode set`, and carries the grant only when
the result is no looser than it. The app is a human GUI with no agent tool path into its
bindings, so unlike the CLI it needs no terminal check to grant. `off` is not a stored
mode and is not offered here; use `claude-off` or `tq off`.

## Install

Download the installer for your OS from the
[latest release](https://github.com/tentaqles/tentaqles/releases/latest).

### Windows

Download `tentaqles-setup-amd64-installer.exe` and run it.

The app isn't code-signed, so Windows SmartScreen will likely warn that the
app is from an "unknown publisher":

1. Click **More info**.
2. Click **Run anyway**.

### macOS

Download `tentaqles-setup.dmg`, open it, and drag the app to Applications.

The app isn't notarized, so Gatekeeper will refuse to open it with a normal
double-click. Either:

- Right-click (or Control-click) the app → **Open** → **Open** in the
  confirmation dialog. This only needs to be done once.
- Or clear the quarantine attribute from a terminal:

  ```sh
  xattr -d com.apple.quarantine /Applications/tentaqles-setup.app
  ```

### Linux

Download `tentaqles-setup.AppImage`, make it executable, and run it:

```sh
chmod +x tentaqles-setup.AppImage
./tentaqles-setup.AppImage
```

## Dev loop

```sh
cd desktop
wails dev
```

This runs the frontend dev server with hot reload and the Go backend
together. Requires Go 1.26+, Node 22, and the Wails CLI
(`go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0`). On Linux
you'll also need `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` (or
`libwebkit2gtk-4.0-dev` on older distros).

To produce a local build (no installer packaging):

```sh
cd desktop
wails build
```

To reproduce the Windows installer locally you also need NSIS
(`choco install nsis`) and a `tq.exe` copied into `desktop/build/bin/`
before building. `desktop/build/windows/installer/project.nsi` picks it up
with a `File /nonfatal "..\..\bin\tq.exe"` line, so `wails build -nsis`
installs it beside the app. This is what the release CI does automatically,
pulling the matching `tq` binary from the `goreleaser` job's GitHub release
assets. To confirm it made it in:

```powershell
& "C:\Program Files\7-Zip\7z.exe" l build\bin\tentaqles-setup-amd64-installer.exe
```

The frontend must be built (`npm run build` in `desktop/frontend`) before
any Go build or test in `desktop/`, because `main.go` uses
`//go:embed all:frontend/dist`.

## Tests

```sh
cd desktop/frontend && npm ci && npx tsc --noEmit && npm test
cd desktop && go vet ./... && go test ./...
```

These run in CI (`.github/workflows/test.yml`, `desktop` job) on every push
and PR. Full installer packaging only runs on tag pushes
(`.github/workflows/release.yml`, `desktop` job), matching the CLI's own
release flow.
