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
install it. If a `tq` binary is bundled next to the app (the release
installers ship one), clicking "Install tq" copies it into `tq`'s per-user
install directory. Otherwise the banner falls back to the published
one-liner installers (`curl ... | sh` / `irm ... | iex`).

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
together. Requires Go 1.22+, Node 22, and the Wails CLI
(`go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0`). On Linux
you'll also need `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` (or
`libwebkit2gtk-4.0-dev` on older distros).

To produce a local build (no installer packaging):

```sh
cd desktop
wails build
```

To reproduce the Windows installer locally you also need NSIS
(`choco install nsis`) and a `tq(.exe)` binary copied into
`desktop/build/bin/` before building, so `wails build -nsis` bundles it
alongside the app (this is what the release CI does automatically, pulling
the matching `tq` binary from the `goreleaser` job's GitHub release
assets).

## Tests

```sh
cd desktop/frontend && npm ci && npx tsc --noEmit && npm test
cd desktop && go vet ./... && go test ./...
```

These run in CI (`.github/workflows/test.yml`, `desktop` job) on every push
and PR. Full installer packaging only runs on tag pushes
(`.github/workflows/release.yml`, `desktop` job), matching the CLI's own
release flow.
