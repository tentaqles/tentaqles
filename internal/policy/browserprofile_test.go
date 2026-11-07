package policy

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/protect"
	"github.com/tentaqles/tentaqles/internal/providers"
)

// The browser rule (ADR-0016): a client with a browsers: list opens pages in its own profile.
// The fixture client lists Chrome with the profile "Profile 2" and nothing else.

func browserScope(t testing.TB, browsers []BrowserProfile) (*Scope, string, string) {
	t.Helper()
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	state := filepath.Join(home, ".tentaqles")
	cwd := filepath.Join(home, "repos", "company-x")
	bdir := filepath.Join(state, "identities", "company-x", "browser")
	env := map[string]string{"TQ_HOME": state}
	set := protect.New(protect.Inputs{Home: state, UserHome: home, Bases: []string{filepath.Join(home, "repos")}, Roots: []string{cwd},
		Env: func(k string) string { return env[k] }})
	cat, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	embedded := map[string]providers.Match{}
	for _, p := range cat.All() {
		embedded[p.ID] = p.Match
	}
	s := NewScope(ScopeInputs{Identity: "company-x", Scope: []string{"az", "azure-devops", "claude"}, Providers: cat.All(), Embedded: embedded,
		Set: set, UserHome: home, Env: func(k string) string { return env[k] }, Browsers: browsers, BrowserDir: bdir})
	return s, cwd, bdir
}

var chromeClient = []BrowserProfile{{Name: "chrome", Profile: "Profile 2"}}

func TestOtherBrowserProfile_Fixtures(t *testing.T) {
	s, cwd, bdir := browserScope(t, chromeClient)
	deny := []ToolCall{
		bash(`chrome --profile-directory=Default`),
		bash(`chrome`),
		bash(`chrome https://example.com`),
		bash(`google-chrome --user-data-dir=/tmp/other`),
		bash(`"C:\Program Files\Google\Chrome\Application\chrome.exe" --user-data-dir=C:\other`),
		bash(`msedge https://x`),
		bash(`msedge --profile-directory="Profile 2"`),
		bash(`google-chrome-stable --profile-directory="Default" https://x`),
		bash(`/usr/bin/google-chrome --incognito`),
		bash(`chrome --profile-directory=Default --user-data-dir=` + bdir),
		bash(`chrome --profile-directory="Profile 2" --user-data-dir=/tmp/other`),
		bash(`cd /tmp && chrome https://x`),
		bash(`echo hi; chrome --profile-directory=Default`),
		bash(`microsoft-edge https://x`),
		bash(`start https://x`),
		bash(`start "" https://x`),
		bash(`start chrome https://x`),
		bash(`cmd /c start https://x`),
		bash(`explorer https://x`),
		bash(`rundll32 url.dll,OpenURL https://x`),
		bash(`CHROME --profile-directory=Default`),
		pwsh(`Start-Process https://x`),
		pwsh(`Start-Process "https://x"`),
		pwsh(`Start-Process chrome -ArgumentList "--profile-directory=Default https://x"`),
		pwsh(`Start-Process -FilePath msedge`),
		pwsh(`& "C:\Program Files\Google\Chrome\Application\chrome.exe" --profile-directory=Default`),
		pwsh(`explorer.exe https://x`),
		pwsh(`rundll32.exe url.dll,FileProtocolHandler https://x`),
		pwsh(`saps https://x`),
		pwsh(`start https://x`),
		pwsh(`cmd /c start https://x`),
		pwsh(`Start-Process chrome`),
		// round 3, L-11: the plain forms of REQ-9 deny exactly as written
		bash(`start https://x`),
		pwsh(`Start-Process https://x`),
		bash(`explorer https://x`),
		bash(`rundll32 url.dll,FileProtocolHandler https://x`),
		bash(`cmd /c start https://x`),
		pwsh(`Start-Process explorer https://x`),
		pwsh(`Start-Process explorer -ArgumentList https://x`),
		pwsh(`Start-Process rundll32 -ArgumentList "url.dll,FileProtocolHandler https://x"`),
		// macOS: the browsers by their app bundle path (what internal/browser launches)
		bash(`"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --profile-directory=Default`),
		bash(`"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"`),
		bash(`"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge" --profile-directory="Profile 2"`),
		// round 1, L-09: a headless start with another profile is still denied
		bash(`chrome --headless --profile-directory=Default https://x`),
		bash(`msedge --version --profile-directory=Default`),
		bash(`google-chrome --headless=new --user-data-dir=/tmp/other`),
		// a cmd start title is not the target
		bash(`start "My title" https://x`),
	}
	if len(deny) < 15 {
		t.Fatal("at least 15 commands must deny")
	}
	for _, c := range deny {
		d := s.Evaluate(cwd, c)
		if d.Action != Deny || len(d.Matched) != 1 || d.Matched[0].ID != OtherBrowserProfileID || d.Matched[0].Category != CategoryIdentity {
			t.Errorf("%s %q: want identity/%s, got %+v", c.Tool, c.Command, OtherBrowserProfileID, d)
			continue
		}
		if !strings.Contains(d.Reason(), "tentaqles open <url>") {
			t.Errorf("%q: the reason does not name tentaqles open <url>: %s", c.Command, d.Reason())
		}
	}
	allow := []ToolCall{
		bash(`chrome --profile-directory="Profile 2" https://x`),
		bash(`chrome --profile-directory="Profile 2"`),
		bash(`google-chrome --profile-directory="Profile 2" https://x`),
		bash(`"C:\Program Files\Google\Chrome\Application\chrome.exe" --profile-directory="Profile 2"`),
		bash(`chrome --user-data-dir=` + bdir),
		bash(`chrome --user-data-dir=` + bdir + ` --profile-directory="Profile 2"`),
		bash(`git commit -m "open chrome --profile-directory=x"`),
		bash(`grep start README.md`),
		bash(`echo https://x`),
		bash(`npm start`),
		bash(`start.sh`),
		bash(`./start.sh https://x`),
		bash(`git status`),
		bash(`git log --grep="chrome --profile-directory=Default"`),
		bash(`cat chrome.txt`),
		bash(`which chrome`),
		bash(`echo "start https://x"`),
		bash(`explorer .`),
		bash(`start notepad`),
		bash(`cmd /c dir`),
		bash(`rundll32 user32.dll,LockWorkStation`),
		bash(`curl https://example.com`),
		bash(`go test ./...`),
		pwsh(`Start-Process notepad`),
		pwsh(`Start-Process notepad -ArgumentList "file.txt"`),
		pwsh(`Write-Host "chrome https://x"`),
		pwsh(`Select-String start README.md`),
		pwsh(`Start-Process chrome -ArgumentList '--profile-directory="Profile 2"'`),
		pwsh(`& "C:\Program Files\Google\Chrome\Application\chrome.exe" --profile-directory="Profile 2"`),
		pwsh(`git commit -m "start https://x"`),
		bash(`"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --profile-directory="Profile 2" https://x`),
		// round 1, L-09 (owner): --version, --help and --headless run with no profile flag
		bash(`chrome --version`),
		bash(`chrome --help`),
		bash(`chrome --headless --dump-dom https://x`),
		bash(`google-chrome --headless=new --screenshot https://x`),
		bash(`msedge --version`),
		bash(`microsoft-edge --help`),
		bash(`"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --version`),
		pwsh(`& "C:\Program Files\Google\Chrome\Application\chrome.exe" --version`),
	}
	if len(allow) < 15 {
		t.Fatal("at least 15 commands must allow")
	}
	for _, c := range allow {
		if d := s.Evaluate(cwd, c); d.Action == Deny {
			t.Errorf("%s %q must pass: %s", c.Tool, c.Command, d.Reason())
		}
	}
}

// Everyday commands pass (REQ-8, REQ-9).
func TestOtherBrowserProfile_FalsePositives(t *testing.T) {
	s, cwd, _ := browserScope(t, chromeClient)
	for _, c := range []ToolCall{
		bash(`git commit -m "open chrome --profile-directory=x"`),
		bash(`grep start README.md`),
		bash(`echo https://x`),
		bash(`npm start`),
		pwsh(`Start-Process notepad`),
		bash(`start.sh`),
		// round 1, L-03: the arguments of another program are that program's
		bash(`start npx playwright test --browser chrome`),
		pwsh(`Start-Process npm -ArgumentList "run test -- --browser chrome"`),
		bash(`start curl https://example.com`),
		pwsh(`Start-Process curl -ArgumentList "https://example.com"`),
		pwsh(`Start-Process -ArgumentList "https://example.com" -FilePath curl`),
		pwsh(`Start-Process curl https://example.com`),
		// round 1, L-09 (owner)
		bash(`chrome --version`),
		bash(`msedge --headless https://x`),
	} {
		if d := s.Evaluate(cwd, c); d.Action == Deny {
			t.Errorf("%s %q must pass: %s", c.Tool, c.Command, d.Reason())
		}
	}
}

// With no browsers: list nothing of the rule applies (REQ-13).
func TestOtherBrowserProfile_NoBlockNoChange(t *testing.T) {
	s, cwd, _ := browserScope(t, nil)
	for _, c := range []ToolCall{
		bash(`chrome --profile-directory=Default`), bash(`chrome https://x`), bash(`msedge`), bash(`start https://x`),
		bash(`explorer https://x`), bash(`cmd /c start https://x`), pwsh(`Start-Process https://x`),
		bash(`rundll32 url.dll,OpenURL https://x`), {Tool: "mcp__claude-in-chrome__navigate", Content: `{}`},
	} {
		if d := s.Evaluate(cwd, c); d.Action == Deny {
			t.Errorf("%s %q must pass for a client with no browsers: %s", c.Tool, c.Command, d.Reason())
		}
	}
}

func TestOtherBrowserProfile_ClaudeInChrome(t *testing.T) {
	call := ToolCall{Tool: "mcp__claude-in-chrome__navigate", Content: `{"url":"https://x"}`}
	edge, cwd, _ := browserScope(t, []BrowserProfile{{Name: "edge", Profile: "Default"}})
	d := edge.Evaluate(cwd, call)
	if d.Action != Deny || d.Matched[0].ID != OtherBrowserProfileID {
		t.Fatalf("an Edge client must be denied Claude in Chrome: %+v", d)
	}
	chrome, cwd, _ := browserScope(t, chromeClient)
	if d := chrome.Evaluate(cwd, call); d.Action == Deny {
		t.Fatalf("a Chrome client may use Claude in Chrome: %s", d.Reason())
	}
}
