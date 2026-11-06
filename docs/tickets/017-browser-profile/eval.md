# 017: The right browser on the right profile: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use a fake browser program that records its argv. No real browser opens in tests.

## Not covered

Which profile a running Claude in Chrome uses can't be observed by tq, and REQ-12 documents that.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 A URL opens the client's profile
- **Covers**: REQ-1
- **Method**: test TestOpen_UsesTheClientsProfile
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOpen_UsesTheClientsProfile$' -count=1`
- **Expect**: For Chrome and Edge the fake records `--profile-directory=Profile 2` and the URL, and `--browser edge` picks Edge.

### U-02 BROWSER, xdg-open and open follow it
- **Covers**: REQ-5, REQ-6, REQ-7
- **Method**: test TestBrowserShims
- **Steps**:
  1. `go test ./internal/cmd ./internal/envplan -run '^TestBrowserShims$' -count=1`
- **Expect**: `BROWSER` names the shim, an https URL reaches the profile, `open file.txt` reaches the real program, and leaving restores `BROWSER` byte for byte.

### U-03 Playwright gets the client's folder
- **Covers**: REQ-10
- **Method**: test TestSettingsRender_PlaywrightUserDataDir
- **Steps**:
  1. `go test ./internal/cmd -run '^TestSettingsRender_PlaywrightUserDataDir$' -count=1`
- **Expect**: The entry holds `--user-data-dir` under the identity and keeps its other arguments, and a second render prints `up to date`.

### U-04 The owner opens a real window
- **Covers**: REQ-1
- **Method**: human
- **Steps**:
  1. In a client folder, run `tentaqles open https://example.com`.
- **Expect**: It opens in the named profile, not the default one.

### F-01 Bad targets
- **Covers**: REQ-2, REQ-3, REQ-4
- **Method**: test TestOpen_Refusals
- **Steps**:
  1. `go test ./internal/cmd -run '^TestOpen_Refusals$' -count=1`
  2. Pass `--user-data-dir=x`, `file:///etc/passwd`, `javascript:alert(1)`, `a & calc`; a neutral folder; no browsers; a browser not installed.
- **Expect**: Each exits non-zero with its message, and the fake records nothing.

### F-02 A missing profile
- **Covers**: REQ-14
- **Method**: test TestDoctor_BrowserProfileMissing
- **Steps**:
  1. `go test ./internal/doctor -run '^TestDoctor_BrowserProfileMissing$' -count=1`
- **Expect**: The warning names the profile and the fix.

### Q-01 The rule has fixtures
- **Covers**: REQ-8, REQ-9
- **Method**: test TestOtherBrowserProfile_Fixtures
- **Steps**:
  1. `go test ./internal/policy -run '^TestOtherBrowserProfile_Fixtures$' -count=1`
- **Expect**: At least 15 commands that must deny and 15 that must allow pass, for bash and PowerShell quoting.

### R-01 No browsers, no change
- **Covers**: REQ-13
- **Method**: test TestBrowser_NoBlockNoChange
- **Steps**:
  1. `go test ./internal/cmd ./internal/envplan -run '^TestBrowser_NoBlockNoChange$' -count=1`
- **Expect**: Environment, PATH and every hook decision are identical to a client with no block before this ticket.

### R-02 Everyday commands pass
- **Covers**: REQ-8, REQ-9
- **Method**: test TestOtherBrowserProfile_FalsePositives
- **Steps**:
  1. `go test ./internal/policy -run '^TestOtherBrowserProfile_FalsePositives$' -count=1`
- **Expect**: `git commit -m "open chrome --profile-directory=x"`, `grep start README.md`, `echo https://x`, `npm start`, `Start-Process notepad` and `start.sh` all allow.

### R-03 The scope rules stay
- **Covers**: REQ-8
- **Method**: test TestScope_EveryProvider
- **Steps**:
  1. `go test ./internal/policy ./internal/envplan -count=1`
- **Expect**: `ok` for both.

### S-01 Another profile is denied
- **Covers**: REQ-8, REQ-15
- **Method**: test TestPreToolUse_OtherBrowserProfileDenied
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_OtherBrowserProfileDenied$' -count=1`
  2. Feed the hook `chrome --profile-directory=Default`, `"C:\Program Files\Google\Chrome\Application\chrome.exe" --user-data-dir=C:\other` and `msedge https://x`.
- **Expect**: Each exits 2 with `BLOCKED [identity/other-browser-profile]`, and logs one line with category `identity`.

### S-02 OS URL routes are denied
- **Covers**: REQ-9
- **Method**: test TestPreToolUse_OsUrlOpenDenied
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_OsUrlOpenDenied$' -count=1`
  2. Feed `start https://x`, `Start-Process https://x`, `explorer https://x`, `cmd /c start https://x` and `rundll32 url.dll,OpenURL https://x`.
- **Expect**: Each exits 2, and the reason names `tentaqles open`.

### S-03 Claude in Chrome on a client without Chrome
- **Covers**: REQ-11, REQ-12
- **Method**: test TestPreToolUse_ClaudeInChromeNeedsChrome
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_ClaudeInChromeNeedsChrome$' -count=1`
- **Expect**: On an Edge client it denies, on a Chrome client it allows, and `docs/COMMANDS.md` states the limit.

### S-04 The shims are protected and signed
- **Covers**: REQ-7
- **Method**: test TestBrowserShims_ProtectedAndSigned
- **Steps**:
  1. `go test ./internal/cmd -run '^TestBrowserShims_ProtectedAndSigned$' -count=1`
- **Expect**: A Write call and a redirect on the shim are denied with `BLOCKED [self-protection/tq/protected-path]`, and a changed shim makes the client untrusted.
