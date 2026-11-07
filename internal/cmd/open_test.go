package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/browser"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/mcpscope"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// The browser profile (ADR-0016) end to end, with a fake browser: the test binary copied as
// chrome-fake / msedge-fake into a folder named fakebin, which records its argv. No real
// browser opens in these tests.

const browserBody = "git: { email: dev@company-x.example }\nidentities: { claude: {} }\n" +
	"browsers:\n  - name: chrome\n    profile: Profile 2\n  - name: edge\n    profile: Profile 3\n"

const edgeOnlyBody = "git: { email: dev@company-x.example }\nidentities: { claude: {} }\n" +
	"browsers:\n  - name: edge\n    profile: Default\n"

const chromeOnlyBody = "git: { email: dev@company-x.example }\nidentities: { claude: {} }\n" +
	"browsers:\n  - name: chrome\n    profile: Profile 2\n"

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func isFakeBrowser() bool {
	exe, err := os.Executable()
	return err == nil && filepath.Base(filepath.Dir(exe)) == "fakebin"
}

// recordFakeBrowser appends the fake's name and arguments to $TQ_TEST_FAKE_LOG as one JSON line.
func recordFakeBrowser() {
	exe, _ := os.Executable()
	line, _ := json.Marshal(fakeRun{Exe: strings.TrimSuffix(strings.ToLower(filepath.Base(exe)), ".exe"), Args: os.Args[1:]})
	if f, err := os.OpenFile(os.Getenv("TQ_TEST_FAKE_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, string(line))
		f.Close()
	}
}

type fakeRun struct {
	Exe  string   `json:"exe"`
	Args []string `json:"args"`
}

type fakeBrowsers struct {
	t   *testing.T
	dir string
	log string
}

// installFakeBrowsers makes chrome and edge installed as copies of the test binary and points
// browser.Locate at them. missing names a browser to leave uninstalled.
func installFakeBrowsers(t *testing.T, missing ...string) *fakeBrowsers {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "fakebin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	where := map[string]string{"chrome": "chrome-fake" + exeSuffix(), "edge": "msedge-fake" + exeSuffix()}
	for name, file := range where {
		if slices.Contains(missing, name) {
			continue
		}
		p := filepath.Join(dir, file)
		if os.Link(self, p) != nil {
			b, err := os.ReadFile(self)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, b, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	f := &fakeBrowsers{t: t, dir: dir, log: filepath.Join(dir, "log.jsonl")}
	t.Setenv("TQ_TEST_FAKE_LOG", f.log)
	prev := browser.Locate
	browser.Locate = func(name string) []string { return []string{filepath.Join(dir, where[name])} }
	t.Cleanup(func() { browser.Locate = prev })
	return f
}

// runs waits for n recorded runs and returns them (n 0: the runs so far, after a short wait).
func (f *fakeBrowsers) runs(n int) []fakeRun {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	if n == 0 {
		time.Sleep(300 * time.Millisecond)
		deadline = time.Now()
	}
	for {
		var out []fakeRun
		if b, err := os.ReadFile(f.log); err == nil {
			for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				var r fakeRun
				if l != "" && json.Unmarshal([]byte(l), &r) == nil {
					out = append(out, r)
				}
			}
		}
		if len(out) >= n || time.Now().After(deadline) {
			return out
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// openIn runs `tentaqles open args...` from dir and returns the exit code and what it printed.
func openIn(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	t.Chdir(dir)
	var out, errb bytes.Buffer
	code := RunMain(append([]string{"open"}, args...), &out, &errb)
	return code, out.String() + errb.String()
}

func TestOpen_UsesTheClientsProfile(t *testing.T) {
	root, _ := setupScoped(t, "company-x", browserBody)
	fake := installFakeBrowsers(t)

	url := "https://example.com/a?b=1&c=2"
	code, out := openIn(t, root, url)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	got := fake.runs(1)
	if len(got) != 1 || got[0].Exe != "chrome-fake" || !slices.Equal(got[0].Args, []string{"--profile-directory=Profile 2", url}) {
		t.Fatalf("chrome was run as %+v", got)
	}
	// the first listed is the default; a second browser by name
	code, out = openIn(t, root, "--browser", "edge", "http://example.org/")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	got = fake.runs(2)
	if len(got) != 2 || got[1].Exe != "msedge-fake" || !slices.Equal(got[1].Args, []string{"--profile-directory=Profile 3", "http://example.org/"}) {
		t.Fatalf("edge was run as %+v", got)
	}
	if code, out = openIn(t, root, "--browser=edge", "https://example.net/"); code != 0 {
		t.Fatalf("--browser=edge: exit %d: %s", code, out)
	}
	// a subfolder of the client is the client
	sub := filepath.Join(root, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, out = openIn(t, sub, "https://example.com/"); code != 0 {
		t.Fatalf("from a subfolder: exit %d: %s", code, out)
	}
	if got = fake.runs(4); len(got) != 4 {
		t.Fatalf("runs = %+v", got)
	}
}

func TestOpen_Refusals(t *testing.T) {
	root, _ := setupScoped(t, "company-x", browserBody)
	fake := installFakeBrowsers(t)
	neutral := t.TempDir()

	cases := []struct {
		name, dir string
		args      []string
		want      string
	}{
		{"a flag as the value", root, []string{"--user-data-dir=x"}, "starts with '-'"},
		{"a flag after --", root, []string{"--", "--user-data-dir=x"}, "starts with '-'"},
		{"a flag beside a URL", root, []string{"--user-data-dir=x", "https://example.com"}, "usage"},
		{"a file URL", root, []string{"file:///etc/passwd"}, "not an http or https URL"},
		{"a javascript URL", root, []string{"javascript:alert(1)"}, "not an http or https URL"},
		{"a shell word", root, []string{"a & calc"}, "refused"},
		{"a URL with a space", root, []string{"https://example.com/a b"}, "refused"},
		{"a quote in the URL", root, []string{`https://example.com/"x`}, "refused"},
		{"no URL", root, nil, "usage"},
		{"two URLs", root, []string{"https://a.example", "https://b.example"}, "usage"},
		{"a browser the client does not list", root, []string{"--browser", "firefox", "https://example.com"}, "firefox is not one of this client's browsers"},
		{"a neutral folder", neutral, []string{"https://example.com"}, "not a client folder"},
	}
	for _, c := range cases {
		code, out := openIn(t, c.dir, c.args...)
		if code != 1 || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, output %q, want exit 1 with %q", c.name, code, out, c.want)
		}
	}

	// no browsers: list
	setupScopedAgain := func(body string) string {
		r, _ := setupScoped(t, "company-y", body)
		return r
	}
	plain := setupScopedAgain(companyXBody)
	if code, out := openIn(t, plain, "https://example.com"); code != 1 || !strings.Contains(out, "no browser is set for this client") {
		t.Errorf("no browsers: exit %d, %q", code, out)
	}

	// listed but not installed
	missing := installFakeBrowsers(t, "edge")
	edge := setupScopedAgain(edgeOnlyBody)
	if code, out := openIn(t, edge, "https://example.com"); code != 1 || !strings.Contains(out, "edge is not installed") {
		t.Errorf("not installed: exit %d, %q", code, out)
	}
	if got := fake.runs(0); len(got) != 0 {
		t.Fatalf("a refusal started a browser: %+v", got)
	}
	if got := missing.runs(0); len(got) != 0 {
		t.Fatalf("a refusal started a browser: %+v", got)
	}
}

// BROWSER, xdg-open and open follow the client's profile (REQ-5, REQ-6, REQ-7).
func TestBrowserShims(t *testing.T) {
	root, _ := setupScoped(t, "company-x", browserBody)
	fake := installFakeBrowsers(t)
	ws := workspaceNamed(t, "company-x")

	// tq settings render writes the shims, signs them, and a second render finds nothing to do
	out := runSettingsRender(t, "company-x")
	if !strings.Contains(out, "browser shims written") {
		t.Fatalf("render did not write the shims:\n%s", out)
	}
	if !envplan.ShimsReady("company-x") {
		t.Fatal("the shims are not ready after a render")
	}
	if out := runSettingsRender(t, "company-x"); strings.Contains(out, "browser shims written") {
		t.Fatalf("a second render wrote them again:\n%s", out)
	}
	ws = workspaceNamed(t, "company-x")
	if v := trust.Check("company-x", ws.Hash); !v.OK {
		t.Fatalf("the client is not trusted after the render signed the shims: %s", v.Reason)
	}

	// the environment of a launch names the shim and puts the folder first on PATH
	t.Setenv("PATH", filepath.Dir(os.Args[0]))
	env := envplan.Environ(ws, os.Environ())
	var browserVar, pathVar string
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, "BROWSER="):
			browserVar = strings.TrimPrefix(kv, "BROWSER=")
		case strings.EqualFold(strings.SplitN(kv, "=", 2)[0], "PATH"):
			pathVar = strings.SplitN(kv, "=", 2)[1]
		}
	}
	shims := paths.ShimsDir("company-x")
	if !strings.HasPrefix(strings.ToLower(filepath.ToSlash(browserVar)), strings.ToLower(filepath.ToSlash(shims))+"/") {
		t.Fatalf("BROWSER = %q, want a program in %s", browserVar, shims)
	}
	if first := filepath.SplitList(pathVar)[0]; !strings.EqualFold(filepath.Clean(first), filepath.Clean(shims)) {
		t.Fatalf("the first PATH entry is %q, want %s", first, shims)
	}

	// an https URL reaches the client's profile through every shim (in this process)
	url := "https://example.com/path?q=1&r=2"
	for _, n := range []string{envplan.ShimOpen, "xdg-open", "open"} {
		var stderr bytes.Buffer
		exe := filepath.Join(shims, n+exeSuffix())
		if ok, code := runShim(exe, []string{url}, &stderr); !ok || code != 0 {
			t.Fatalf("%s %s: ok=%v code=%d %s", n, url, ok, code, stderr.String())
		}
	}
	got := fake.runs(3)
	if len(got) != 3 {
		t.Fatalf("runs = %+v", got)
	}
	for _, r := range got {
		if r.Exe != "chrome-fake" || !slices.Equal(r.Args, []string{"--profile-directory=Profile 2", url}) {
			t.Fatalf("a shim started %+v", r)
		}
	}

	// tq-open takes only a URL; xdg-open and open hand anything else to the real program,
	// here a fake `open` on PATH after the shims folder, run as a real process
	var stderr bytes.Buffer
	if ok, code := runShim(filepath.Join(shims, envplan.ShimOpen+exeSuffix()), []string{"file.txt"}, &stderr); !ok || code == 0 {
		t.Fatalf("tq-open file.txt: ok=%v code=%d", ok, code)
	}
	if ok, code := runShim(filepath.Join(shims, envplan.ShimOpen+exeSuffix()), nil, &stderr); !ok || code != 2 {
		t.Fatalf("tq-open with no URL: ok=%v code=%d", ok, code)
	}
	if ok, code := runShim(filepath.Join(shims, envplan.ShimOpen+exeSuffix()), []string{"-evil"}, &stderr); !ok || code == 0 {
		t.Fatalf("tq-open -evil: ok=%v code=%d", ok, code)
	}
	realDir := filepath.Join(t.TempDir(), "fakebin")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	realOpen := filepath.Join(realDir, "open"+exeSuffix())
	if os.Link(self, realOpen) != nil {
		b, _ := os.ReadFile(self)
		if err := os.WriteFile(realOpen, b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	shimOpen := filepath.Join(shims, "open"+exeSuffix())
	cmd := exec.Command(shimOpen, "file.txt")
	cmd.Env = append(withoutPath(os.Environ()), "PATH="+shims+string(os.PathListSeparator)+realDir, "TQ_TEST_FAKE_LOG="+fake.log)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("open file.txt through the shim: %v\n%s", err, out)
	}
	got = fake.runs(4)
	if len(got) != 4 || got[3].Exe != "open" || !slices.Equal(got[3].Args, []string{"file.txt"}) {
		t.Fatalf("open file.txt did not reach the real program: %+v", got)
	}
	// with no real program the shim says so
	cmd = exec.Command(shimOpen, "file.txt")
	cmd.Env = append(withoutPath(os.Environ()), "PATH="+shims)
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "command not found") {
		t.Fatalf("open with no real open: %v %s", err, out)
	}
	_ = root
}

func withoutPath(env []string) []string {
	var out []string
	for _, kv := range env {
		if !strings.EqualFold(strings.SplitN(kv, "=", 2)[0], "PATH") {
			out = append(out, kv)
		}
	}
	return out
}

func workspaceNamed(t *testing.T, name string) *resolve.Workspace {
	t.Helper()
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	all, _ := resolve.ListWorkspaces(cfg)
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	t.Fatalf("no workspace %s", name)
	return nil
}

func runSettingsRender(t *testing.T, names ...string) string {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"settings", "render"}, names...))
	if err := root.Execute(); err != nil {
		t.Fatalf("settings render: %v\n%s", err, out.String())
	}
	return out.String()
}

func TestSettingsRender_PlaywrightUserDataDir(t *testing.T) {
	setupScoped(t, "company-x", browserBody+"mcp:\n  allow: [acme-tool]\n")
	claudeDir := paths.IdentityDir("company-x", "claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(claudeDir, ".claude.json")
	orig := `{
  "numStartups": 12,
  "mcpServers": {
    "playwright": { "type": "stdio", "command": "npx", "args": ["@playwright/mcp@0.0.40", "--headless", "--user-data-dir", "C:\\somewhere"] },
    "acme-tool": { "command": "acme-tool", "args": ["serve"] }
  },
  "projects": {
    "C:/work/app": {
      "mcpServers": { "pw": { "command": "npx", "args": ["-y", "@playwright/mcp@latest", "--browser", "chromium"] } }
    }
  }
}
`
	if err := os.WriteFile(cfgFile, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runSettingsRender(t, "company-x")
	dir := paths.BrowserDir("company-x")
	if !strings.Contains(out, "--user-data-dir") {
		t.Fatalf("render did not report the Playwright change:\n%s", out)
	}
	raw, _ := os.ReadFile(cfgFile)
	var doc struct {
		NumStartups int                       `json:"numStartups"`
		MCPServers  map[string]map[string]any `json:"mcpServers"`
		Projects    map[string]struct {
			MCPServers map[string]map[string]any `json:"mcpServers"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the config is not JSON: %v\n%s", err, raw)
	}
	pw := mcpscope.DataDirOf(doc.MCPServers["playwright"])
	pw2 := mcpscope.DataDirOf(doc.Projects["C:/work/app"].MCPServers["pw"])
	if pw != dir || pw2 != dir {
		t.Fatalf("--user-data-dir = %q, %q; want %q", pw, pw2, dir)
	}
	args := fmt.Sprint(doc.MCPServers["playwright"]["args"])
	if !strings.Contains(args, "@playwright/mcp@0.0.40") || !strings.Contains(args, "--headless") || strings.Contains(args, "somewhere") {
		t.Fatalf("the user server's arguments: %s", args)
	}
	args2 := fmt.Sprint(doc.Projects["C:/work/app"].MCPServers["pw"]["args"])
	if !strings.Contains(args2, "-y @playwright/mcp@latest --browser chromium") {
		t.Fatalf("the project server's arguments: %s", args2)
	}
	if fmt.Sprint(doc.MCPServers["acme-tool"]["args"]) != "[serve]" || doc.NumStartups != 12 {
		t.Fatalf("an entry that is not Playwright changed: %s", raw)
	}
	// a second render changes nothing
	before := string(raw)
	out = runSettingsRender(t, "company-x")
	if !strings.Contains(out, "up to date") || strings.Contains(out, "--user-data-dir") {
		t.Fatalf("the second render:\n%s", out)
	}
	if after, _ := os.ReadFile(cfgFile); string(after) != before {
		t.Fatal("the second render rewrote the config")
	}
	// a client with no browsers: its Playwright entry is left as it is
	setupScoped(t, "company-z", companyXBody)
	zdir := paths.IdentityDir("company-z", "claude")
	if err := os.MkdirAll(zdir, 0o700); err != nil {
		t.Fatal(err)
	}
	zorig := `{"mcpServers":{"playwright":{"command":"npx","args":["@playwright/mcp@latest"]}}}`
	if err := os.WriteFile(filepath.Join(zdir, ".claude.json"), []byte(zorig), 0o600); err != nil {
		t.Fatal(err)
	}
	runSettingsRender(t, "company-z")
	if b, _ := os.ReadFile(filepath.Join(zdir, ".claude.json")); string(b) != zorig {
		t.Fatalf("a client with no browsers had its Playwright entry changed: %s", b)
	}
}

// The hook: another profile is denied, with the rule's name, and the denial is logged (REQ-8, REQ-15).
func TestPreToolUse_OtherBrowserProfileDenied(t *testing.T) {
	root, _ := setupScoped(t, "company-x", chromeOnlyBody)
	for _, c := range []string{
		`chrome --profile-directory=Default`,
		`"C:\Program Files\Google\Chrome\Application\chrome.exe" --user-data-dir=C:\other`,
		`msedge https://x`,
	} {
		code, category, rule, errOut := hookDecision(t, root, "Bash", cmdIn(c))
		if code != 2 || category != policy.CategoryIdentity || rule != policy.OtherBrowserProfileID || !strings.Contains(errOut, "BLOCKED [identity/other-browser-profile]") {
			t.Errorf("%s: code=%d category=%q rule=%q: %s", c, code, category, rule, errOut)
		}
	}
	if code, _, _, errOut := hookDecision(t, root, "Bash", cmdIn(`chrome --profile-directory="Profile 2" https://x`)); code != 0 {
		t.Errorf("the client's own profile was denied: %s", errOut)
	}
	raw, err := os.ReadFile(filepath.Join(paths.Home(), "denials.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d denial lines, want one per deny: %s", len(lines), raw)
	}
	for _, l := range lines {
		if !strings.Contains(l, `"category":"identity"`) || !strings.Contains(l, `"rule":"other-browser-profile"`) {
			t.Errorf("the denial line: %s", l)
		}
	}
}

func TestPreToolUse_OsUrlOpenDenied(t *testing.T) {
	root, _ := setupScoped(t, "company-x", chromeOnlyBody)
	for _, c := range []struct{ tool, cmd string }{
		{"Bash", `start https://x`},
		{"PowerShell", `Start-Process https://x`},
		{"Bash", `explorer https://x`},
		{"Bash", `cmd /c start https://x`},
		{"Bash", `rundll32 url.dll,OpenURL https://x`},
	} {
		code, _, rule, errOut := hookDecision(t, root, c.tool, cmdIn(c.cmd))
		if code != 2 || rule != policy.OtherBrowserProfileID || !strings.Contains(errOut, "tentaqles open") {
			t.Errorf("%s: code=%d rule=%q: %s", c.cmd, code, rule, errOut)
		}
	}
}

func TestPreToolUse_ClaudeInChromeNeedsChrome(t *testing.T) {
	call := `{"url":"https://x"}`
	edge, _ := setupScoped(t, "company-x", edgeOnlyBody)
	code, category, rule, errOut := hookDecision(t, edge, "mcp__claude-in-chrome__navigate", call)
	if code != 2 || category != policy.CategoryIdentity || rule != policy.OtherBrowserProfileID {
		t.Fatalf("an Edge client: code=%d %q %q: %s", code, category, rule, errOut)
	}
	chrome, _ := setupScoped(t, "company-y", chromeOnlyBody)
	if code, _, _, errOut := hookDecision(t, chrome, "mcp__claude-in-chrome__navigate", call); code != 0 {
		t.Fatalf("a Chrome client was denied: %s", errOut)
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "COMMANDS.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ToLower(string(b))
	if !strings.Contains(doc, "claude in chrome") || !strings.Contains(doc, "already running") || !strings.Contains(doc, "cannot verify") {
		t.Fatal("docs/COMMANDS.md does not state that Claude in Chrome attaches to a running browser whose profile tq cannot verify")
	}
}

// A client with no browsers: block: the hook, the environment and the shims are as before (REQ-13).
func TestBrowser_NoBlockNoChange(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
	for _, c := range []string{`chrome --profile-directory=Default`, `chrome https://x`, `msedge`, `start https://x`, `explorer https://x`, `cmd /c start https://x`} {
		if code, _, _, errOut := hookDecision(t, root, "Bash", cmdIn(c)); code != 0 {
			t.Errorf("%s was denied for a client with no browsers: %s", c, errOut)
		}
	}
	if code, _, _, errOut := hookDecision(t, root, "mcp__claude-in-chrome__navigate", `{}`); code != 0 {
		t.Errorf("Claude in Chrome was denied for a client with no browsers: %s", errOut)
	}
	out := runSettingsRender(t, "company-x")
	if strings.Contains(out, "shims") || strings.Contains(out, "--user-data-dir") {
		t.Fatalf("a render for a client with no browsers did something browser-shaped:\n%s", out)
	}
	if _, err := os.Stat(paths.ShimsDir("company-x")); err == nil {
		t.Fatal("a shims folder was written for a client with no browsers")
	}
	ws := workspaceNamed(t, "company-x")
	for _, kv := range envplan.Environ(ws, os.Environ()) {
		if strings.HasPrefix(kv, "BROWSER=") && strings.Contains(kv, "shims") {
			t.Fatalf("BROWSER names a shim: %s", kv)
		}
		if strings.EqualFold(strings.SplitN(kv, "=", 2)[0], "PATH") && strings.Contains(kv, paths.ShimsRoot()) {
			t.Fatalf("PATH holds a shims folder: %s", kv)
		}
	}
	// tentaqles open says so
	if code, out := openIn(t, root, "https://example.com"); code != 1 || !strings.Contains(out, "no browser is set for this client") {
		t.Fatalf("open: %d %s", code, out)
	}
}

// The shims folder is in the protected path set, and the integrity record covers it (REQ-7).
func TestBrowserShims_ProtectedAndSigned(t *testing.T) {
	root, _ := setupScoped(t, "company-x", browserBody)
	installFakeBrowsers(t)
	runSettingsRender(t, "company-x")
	shim := filepath.Join(paths.ShimsDir("company-x"), "xdg-open"+exeSuffix())
	if _, err := os.Stat(shim); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ tool, input string }{
		{"Write", `{"file_path":` + jsonPathLit(shim) + `,"content":"x"}`},
		{"Edit", `{"file_path":` + jsonPathLit(shim) + `,"old_string":"a","new_string":"b"}`},
		{"Bash", cmdIn(`echo evil > "` + shim + `"`)},
		{"Bash", cmdIn(`cp /tmp/x "` + shim + `"`)},
	} {
		code, category, rule, errOut := hookDecision(t, root, c.tool, c.input)
		if code != 2 || category != policy.CategorySelfProtection || !strings.Contains(errOut, "BLOCKED [self-protection/tq/protected-path]") {
			t.Errorf("%s: code=%d %q %q: %s", c.tool, code, category, rule, errOut)
		}
	}

	// a changed shim makes the client untrusted
	ws := workspaceNamed(t, "company-x")
	if v := trust.Check("company-x", ws.Hash); !v.OK {
		t.Fatalf("not trusted after the render: %s", v.Reason)
	}
	f, err := os.OpenFile(shim, os.O_APPEND|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("tampered")
	f.Close()
	if v := trust.Check("company-x", ws.Hash); v.OK || !strings.Contains(strings.ToLower(filepath.ToSlash(v.Reason)), "shims/company-x/") {
		t.Fatalf("a changed shim left the client trusted: %+v", v)
	}
	cfg, _ := registry.Load()
	if res := resolve.Resolve(root, cfg); res.Workspace != nil || res.Untrusted == nil || !res.Mismatch {
		t.Fatalf("the client still resolves after its shim changed: %+v", res)
	}
	// and the shim itself refuses to open anything
	var stderr bytes.Buffer
	if ok, code := runShim(shim, []string{"https://example.com"}, &stderr); !ok || code == 0 {
		t.Fatalf("a shim of an untrusted client opened a page: ok=%v code=%d", ok, code)
	}
}

// An http or https URL never reaches the real xdg-open or open (round 1, L-01): it opens in the
// client's profile or is refused, whatever else is in it. `open -a` for the two browsers goes
// to the client's profile; for another app it passes through.
func TestBrowserShims_WebURLNeverReachesTheRealProgram(t *testing.T) {
	setupScoped(t, "company-x", browserBody)
	fake := installFakeBrowsers(t)
	runSettingsRender(t, "company-x")
	shims := paths.ShimsDir("company-x")

	realDir := filepath.Join(t.TempDir(), "fakebin")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	for _, n := range []string{"open", "xdg-open"} {
		p := filepath.Join(realDir, n+exeSuffix())
		if os.Link(self, p) != nil {
			b, _ := os.ReadFile(self)
			if err := os.WriteFile(p, b, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", shims+string(os.PathListSeparator)+realDir)

	run := func(shim string, args ...string) int {
		var stderr bytes.Buffer
		ok, code := runShim(filepath.Join(shims, shim+exeSuffix()), args, &stderr)
		if !ok {
			t.Fatalf("%s %v: not a shim", shim, args)
		}
		return code
	}
	profile2 := "--profile-directory=Profile 2"
	profile3 := "--profile-directory=Profile 3"
	var want []fakeRun
	expect := func(r ...fakeRun) {
		t.Helper()
		want = append(want, r...)
		got := fake.runs(len(want))
		if len(got) != len(want) {
			t.Fatalf("runs = %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i].Exe != want[i].Exe || !slices.Equal(got[i].Args, want[i].Args) {
				t.Fatalf("run %d = %+v, want %+v", i, got[i], want[i])
			}
		}
	}

	if c := run("open", "https://en.wikipedia.org/wiki/O'Reilly"); c != 0 {
		t.Fatalf("an apostrophe: exit %d", c)
	}
	expect(fakeRun{"chrome-fake", []string{profile2, "https://en.wikipedia.org/wiki/O'Reilly"}})
	if c := run("xdg-open", "https://fonts.googleapis.com/css?family=A|B"); c != 0 {
		t.Fatalf("a pipe: exit %d", c)
	}
	expect(fakeRun{"chrome-fake", []string{profile2, "https://fonts.googleapis.com/css?family=A|B"}})
	if c := run("open", "https://a.example", "https://b.example"); c != 0 {
		t.Fatalf("two URLs: exit %d", c)
	}
	expect(fakeRun{"chrome-fake", []string{profile2, "https://a.example"}}, fakeRun{"chrome-fake", []string{profile2, "https://b.example"}})
	if c := run("open", "-g", "https://a.example"); c != 0 {
		t.Fatalf("a flag: exit %d", c)
	}
	expect(fakeRun{"chrome-fake", []string{profile2, "https://a.example"}})
	if c := run("open", "HTTPS://UPPER.EXAMPLE/x"); c != 0 {
		t.Fatalf("upper case: exit %d", c)
	}
	expect(fakeRun{"chrome-fake", []string{profile2, "HTTPS://UPPER.EXAMPLE/x"}})

	// open -a for the two browsers: the client's profile in that browser
	if c := run("open", "-a", "Google Chrome", "https://c.example"); c != 0 {
		t.Fatalf("-a Google Chrome: exit %d", c)
	}
	expect(fakeRun{"chrome-fake", []string{profile2, "https://c.example"}})
	if c := run("open", "-a", "Microsoft Edge", "https://e.example"); c != 0 {
		t.Fatalf("-a Microsoft Edge: exit %d", c)
	}
	expect(fakeRun{"msedge-fake", []string{profile3, "https://e.example"}})
	// round 3, L-01: the app given by its path names the same app
	if c := run("open", "-a", "/Applications/Google Chrome.app", "https://p.example"); c != 0 {
		t.Fatalf("-a with a path to Chrome: exit %d", c)
	}
	expect(fakeRun{"chrome-fake", []string{profile2, "https://p.example"}})
	if c := run("open", "-a", "/Applications/Microsoft Edge.app", "https://q.example"); c != 0 {
		t.Fatalf("-a with a path to Edge: exit %d", c)
	}
	expect(fakeRun{"msedge-fake", []string{profile3, "https://q.example"}})

	// open -a for another app is that app's business: the real program runs
	if c := run("open", "-a", "Safari", "https://s.example"); c != 0 {
		t.Fatalf("-a Safari: exit %d", c)
	}
	expect(fakeRun{"open", []string{"-a", "Safari", "https://s.example"}})
	// a line with no URL is the real program's too
	if c := run("open", "notes.txt"); c != 0 {
		t.Fatalf("a file: exit %d", c)
	}
	expect(fakeRun{"open", []string{"notes.txt"}})

	// a URL tq refuses opens nothing, and does not fall through to the real program
	var stderr bytes.Buffer
	ok, code := runShim(filepath.Join(shims, "open"+exeSuffix()), []string{"https://a.example", "https://b.example/a b"}, &stderr)
	if !ok || code == 0 {
		t.Fatalf("a refused URL: ok=%v code=%d", ok, code)
	}
	if got := fake.runs(0); len(got) != len(want) {
		t.Fatalf("a refusal started something: %+v", got[len(want):])
	}
}
