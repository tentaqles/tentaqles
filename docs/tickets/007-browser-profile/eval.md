# 007: One browser profile per identity: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use a fake browser program that records its argv. No real browser opens in tests.

## Not covered

A real Chrome or Edge window is the owner's U-04. Which profile a running Claude in Chrome uses can't be observed by tq, and REQ-9 documents that. Safari and Opera are out of scope.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 A URL opens the identity's profile
- **Covers**: REQ-2, REQ-3
- **Method**: test TestBrowserShim_OpensProfile
- **Steps**:
  1. `go test ./internal/cmd -run '^TestBrowserShim_OpensProfile$' -count=1`
- **Expect**: For Chrome and Edge the fake records `--profile-directory=Profile 2` and the URL, and `BROWSER` names the shim.

### U-02 xdg-open and open follow it
- **Covers**: REQ-4
- **Method**: test TestBrowserShim_OpenAndXdgOpen
- **Steps**:
  1. `go test ./internal/cmd -run '^TestBrowserShim_OpenAndXdgOpen$' -count=1`
- **Expect**: An https URL goes to the profile, and `open file.txt` reaches the real program with that argument.

### U-03 Playwright gets the identity folder
- **Covers**: REQ-7
- **Method**: test TestSettingsRender_PlaywrightUserDataDir
- **Steps**:
  1. `go test ./internal/cmd -run '^TestSettingsRender_PlaywrightUserDataDir$' -count=1`
- **Expect**: The Playwright entry holds `--user-data-dir` under the identity and keeps its other arguments, and a second render prints `up to date`.

### U-04 The owner opens a real window
- **Covers**: REQ-3
- **Method**: human
- **Steps**:
  1. In a flagged identity, run `tq-browser https://example.com`.
- **Expect**: It opens in the named profile, not the default one.

### F-01 A bad manifest
- **Covers**: REQ-1
- **Method**: test TestManifest_BrowserBlock
- **Steps**:
  1. `go test ./internal/manifest -run '^TestManifest_BrowserBlock$' -count=1`
- **Expect**: `safari`, `opera`, an empty name, and a token-shaped profile fail to load naming the field, while chrome and edge load.

### F-02 The browser is not installed
- **Covers**: REQ-3, REQ-11
- **Method**: test TestBrowserShim_NotInstalled
- **Steps**:
  1. `go test ./internal/cmd -run '^TestBrowserShim_NotInstalled$' -count=1`
- **Expect**: The shim exits non-zero with `chrome is not installed`, opens nothing else, and doctor's `browser-profile-missing` names the fix.

### F-03 A hostile URL
- **Covers**: REQ-3
- **Method**: test TestBrowserShim_RefusesNonWebAndFlags
- **Steps**:
  1. `go test ./internal/cmd -run '^TestBrowserShim_RefusesNonWebAndFlags$' -count=1`
  2. Pass `--user-data-dir=x`, `file:///etc/passwd`, `javascript:alert(1)` and `a & calc`.
- **Expect**: Each refuses with exit non-zero, and the fake records nothing.

### Q-01 The rule has fixtures
- **Covers**: REQ-5, REQ-6
- **Method**: test TestOtherBrowserProfile_Fixtures
- **Steps**:
  1. `go test ./internal/policy -run '^TestOtherBrowserProfile_Fixtures$' -count=1`
- **Expect**: A table of commands that must deny and must allow passes, with at least 15 of each, for bash and PowerShell quoting.

### R-01 No flag, no change
- **Covers**: REQ-10
- **Method**: test TestBrowser_NoBlockNoChange
- **Steps**:
  1. `go test ./internal/cmd ./internal/envplan -run '^TestBrowser_NoBlockNoChange$' -count=1`
- **Expect**: Environment, PATH and every hook decision are identical to before, for an unflagged identity and for a flagged one with no block.

### R-02 Everyday commands pass
- **Covers**: REQ-5, REQ-6
- **Method**: test TestOtherBrowserProfile_FalsePositives
- **Steps**:
  1. `go test ./internal/policy -run '^TestOtherBrowserProfile_FalsePositives$' -count=1`
- **Expect**: `git commit -m "open chrome --profile-directory=x"`, `grep start README.md`, `echo https://x`, `npm start`, `Start-Process notepad` and `start.sh` all allow.

### R-03 The scope rules stay
- **Covers**: REQ-5
- **Method**: test TestScope_EveryProvider
- **Steps**:
  1. `go test ./internal/policy ./internal/cmd -count=1`
- **Expect**: `ok` for both.

### S-01 Another profile is denied
- **Covers**: REQ-5, REQ-12
- **Method**: test TestPreToolUse_OtherBrowserProfileDenied
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_OtherBrowserProfileDenied$' -count=1`
  2. Feed the hook `chrome --profile-directory=Default`, `"C:\Program Files\Google\Chrome\Application\chrome.exe" --user-data-dir=C:\other` and `msedge https://x`.
- **Expect**: Each exits 2 with `BLOCKED [identity/other-browser-profile]`, and each logs one line with category `identity`.

### S-02 OS URL routes are denied
- **Covers**: REQ-6
- **Method**: test TestPreToolUse_OsUrlOpenDenied
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_OsUrlOpenDenied$' -count=1`
  2. Feed `start https://x`, `Start-Process https://x`, `explorer https://x`, `cmd /c start https://x` and `rundll32 url.dll,OpenURL https://x`.
- **Expect**: Each exits 2, and the reason names `tq-browser`.

### S-03 Claude in Chrome on a non-Chrome identity
- **Covers**: REQ-8, REQ-9
- **Method**: test TestPreToolUse_ClaudeInChromeNeedsChrome
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_ClaudeInChromeNeedsChrome$' -count=1`
- **Expect**: On an Edge identity it denies, on a Chrome identity it allows, and `docs/COMMANDS.md` states the limit.

### S-04 The shim can't be written by an agent
- **Covers**: REQ-3
- **Method**: test TestProtectedPaths_BrowserShim
- **Steps**:
  1. `go test ./internal/cmd -run '^TestProtectedPaths_BrowserShim$' -count=1`
- **Expect**: A Write call and a redirect on `$TQ_HOME/shims/tq-browser` are denied with `BLOCKED [self-protection/tq/protected-path]`.
