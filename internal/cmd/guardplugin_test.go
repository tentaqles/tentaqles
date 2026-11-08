package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardplugin"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The guard of a session is the tentaqles plugin's PreToolUse hook: tentaqles run refuses
// to start claude in an identity's folder when the client's Claude config has no enabled
// plugin that runs it. These tests write the plugin the way claude plugin install leaves it.

// guardOpts is the client's Claude config as a test writes it. The
// zero value is a real install: the plugin enabled, its hooks as the release ships them.
type guardOpts struct {
	// Absent writes no plugin, no installed_plugins.json and no settings.json.
	Absent bool
	// Disabled writes enabledPlugins with the tentaqles plugin false.
	Disabled bool
	// DisableAllHooks writes disableAllHooks true in settings.json.
	DisableAllHooks bool
	// Hooks is the plugin's hooks/hooks.json; "" is the release's (plugin/hooks/hooks.json).
	Hooks string
	// Identities is the manifest's identities: flow map; "" is claude.
	Identities string
	// NoRegistration writes no installed_plugins.json: claude loads the highest cached version
	// instead (PluginRoot's second source), as the integrity tests of a new plugin version need.
	NoRegistration bool
}

func (g guardOpts) identities() string {
	if g.Identities == "" {
		return "{ claude: {} }"
	}
	return g.Identities
}

// guardVersion is the version folder the fixture installs the plugin in.
const guardVersion = "0.16.0"

// shippedHooks is plugin/hooks/hooks.json, the file the release ships.
func shippedHooks(tb testing.TB) string {
	tb.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "hooks", "hooks.json"))
	if err != nil {
		tb.Fatal(err)
	}
	return string(raw)
}

// writeGuard writes name's Claude config as g says: the plugin folder in the plugin cache, with
// its manifest, hooks and script; installed_plugins.json naming it; and settings.json with
// enabledPlugins.
func writeGuard(tb testing.TB, name string, g guardOpts) {
	tb.Helper()
	if g.Absent {
		return
	}
	claude := paths.IdentityDir(name, "claude")
	plugin := filepath.Join(claude, "plugins", "cache", "tentaqles", "tentaqles", guardVersion)
	hooks := g.Hooks
	if hooks == "" {
		hooks = shippedHooks(tb)
	}
	writeTB(tb, filepath.Join(plugin, ".claude-plugin", "plugin.json"), `{"name":"tentaqles","version":"`+guardVersion+`"}`)
	writeTB(tb, filepath.Join(plugin, "hooks", "hooks.json"), hooks)
	writeTB(tb, filepath.Join(plugin, "scripts", "tq_hook.sh"), "#!/usr/bin/env bash\nexit 0\n")
	if !g.NoRegistration {
		inst := fmt.Sprintf(`{"version":2,"plugins":{"tentaqles@tentaqles":[{"scope":"user","installPath":%s,"version":%q}]}}`,
			jsonPathLit(plugin), guardVersion)
		writeTB(tb, filepath.Join(claude, "plugins", "installed_plugins.json"), inst)
	}
	settings := map[string]any{"enabledPlugins": map[string]any{"tentaqles@tentaqles": !g.Disabled}}
	if g.DisableAllHooks {
		settings["disableAllHooks"] = true
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		tb.Fatal(err)
	}
	writeTB(tb, filepath.Join(claude, "settings.json"), string(raw))
}

func writeTB(tb testing.TB, p, body string) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		tb.Fatal(err)
	}
}

// guardFix is the exact line that installs the plugin for a client (REQ-5).
const guardFix = "tentaqles run acme -- claude plugin install tentaqles@tentaqles"

// guardInstallFix is what a refusal for a missing plugin prints, in this order: the marketplace (it
// may be there already), REQ-5's install line, and the re-sign, since the install changes what the
// record covers.
var guardInstallFix = []string{"tentaqles run acme -- claude plugin marketplace add tentaqles/tentaqles", guardFix}

// fixBlockOf is the refusal's fix section for lines: the form fixBlock prints.
func fixBlockOf(lines []string) string {
	if len(lines) == 1 {
		return "fix: " + lines[0]
	}
	return "fix, in this order:\n    " + strings.Join(lines, "\n    ")
}

// guardCopies are the files a real `claude plugin install` leaves for acme's Claude config, as a
// fake claude copies them into place: the plugin folder, installed_plugins.json and settings.json
// with enabledPlugins. The sources are written to a folder of their own. The copy runs only for the
// install (CopyArg), as the marketplace add changes none of the covered files.
func guardCopies(t *testing.T, name string) []testutil.FakeCopy {
	t.Helper()
	src := testutil.TempDir(t)
	claude := paths.IdentityDir(name, "claude")
	plugin := filepath.Join(claude, "plugins", "cache", "tentaqles", "tentaqles", guardVersion)
	hooks, err := os.ReadFile(filepath.Join("..", "..", "plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"plugin.json":    `{"name":"tentaqles","version":"` + guardVersion + `"}`,
		"hooks.json":     string(hooks),
		"tq_hook.sh":     "#!/usr/bin/env bash\nexit 0\n",
		"installed.json": fmt.Sprintf(`{"version":2,"plugins":{"tentaqles@tentaqles":[{"scope":"user","installPath":%s,"version":%q}]}}`, jsonPathLit(plugin), guardVersion),
		"settings.json":  `{"enabledPlugins":{"tentaqles@tentaqles":true}}`,
	}
	for n, body := range files {
		writeTB(t, filepath.Join(src, n), body)
	}
	return []testutil.FakeCopy{
		{From: filepath.Join(src, "plugin.json"), To: filepath.Join(plugin, ".claude-plugin", "plugin.json")},
		{From: filepath.Join(src, "hooks.json"), To: filepath.Join(plugin, "hooks", "hooks.json")},
		{From: filepath.Join(src, "tq_hook.sh"), To: filepath.Join(plugin, "scripts", "tq_hook.sh")},
		{From: filepath.Join(src, "installed.json"), To: filepath.Join(claude, "plugins", "installed_plugins.json")},
		{From: filepath.Join(src, "settings.json"), To: filepath.Join(claude, "settings.json")},
	}
}

func TestRun_StartsWithTheHook(t *testing.T) {
	mkLaunchWS(t, "acme")
	clearAgentMarkers(t)
	if g := guardplugin.CheckGuardHook(paths.IdentityDir("acme", "claude")); !g.OK {
		t.Fatalf("the fixture is not a guard: %s", g.Why)
	}
	calls := fakeExec(t)
	if out, err := runTq(t, "run", "acme", "--", "claude"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if len(*calls) != 1 || prog((*calls)[0].name) != "claude" {
		t.Fatalf("calls %+v", *calls)
	}
}

// No plugin, no session: tentaqles run <ws> and tentaqles run . refuse with the fix line, and nothing launches.
// The fix line itself runs (TestRun_RefusalOnlyForASession).
func TestRun_RefusesWithNoGuardHook(t *testing.T) {
	root, _ := mkGuardWS(t, "acme", guardOpts{Absent: true})
	clearAgentMarkers(t)
	calls := fakeExec(t)
	for _, args := range [][]string{{"run", "acme", "--", "claude"}, {"run", "acme", "--", "claude", "-p", "hi"}} {
		_, err := runTq(t, args...)
		if err == nil || !strings.Contains(err.Error(), "without the tentaqles guard") || !strings.Contains(err.Error(), "\n  "+fixBlockOf(guardInstallFix)) {
			t.Fatalf("%v: want the refusal with the fix lines in order, got %v", args, err)
		}
	}
	chdir(t, root)
	_, err := runTq(t, "run", ".", "--", "claude")
	if err == nil || !strings.Contains(err.Error(), "\n  "+fixBlockOf(guardInstallFix)) {
		t.Fatalf("tentaqles run . : want the refusal with the fix lines in order, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing may launch without the guard: %+v", *calls)
	}
}

// A plugin in the identity's Claude dir does not count when the session would run with no Claude
// config: a client without a claude identity has no CLAUDE_CONFIG_DIR, so claude would read the
// user's own config, which holds no guard of tq's.
func TestRun_NoClaudeIdentityIsNoGuard(t *testing.T) {
	mkGuardWS(t, "acme", guardOpts{Identities: "{ gh: {} }"})
	clearAgentMarkers(t)
	calls := fakeExec(t)
	_, err := runTq(t, "run", "acme", "--", "claude")
	if err == nil || !strings.Contains(err.Error(), "no claude identity") || len(*calls) != 0 {
		t.Fatalf("want the refusal, got %v (calls %+v)", err, *calls)
	}
}

// Disabled, disableAllHooks and a plugin with no PreToolUse hook each count as no hook; the
// disableAllHooks line names the setting.
func TestRun_DisabledPluginIsNoHook(t *testing.T) {
	cases := []struct {
		name string
		g    guardOpts
		want []string
		// fix is the fix section the refusal must print.
		fix string
	}{
		{"enabledPlugins false", guardOpts{Disabled: true}, []string{"not enabled", "enabledPlugins"},
			"fix: tentaqles run acme -- claude plugin enable tentaqles@tentaqles"},
		{"disableAllHooks", guardOpts{DisableAllHooks: true}, []string{"disableAllHooks", "settings.json", "turns off every hook"},
			`fix: remove "disableAllHooks" (or set it to false) in `},
		{"no PreToolUse", guardOpts{Hooks: `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"bash \"${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh\" session-start"}]}]}}`},
			[]string{"no PreToolUse hook"}, "fix, in this order:\n    tentaqles run acme -- claude plugin uninstall tentaqles@tentaqles\n    tentaqles run acme -- claude plugin marketplace add tentaqles/tentaqles\n    tentaqles run acme -- claude plugin install tentaqles@tentaqles"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mkGuardWS(t, "acme", c.g)
			clearAgentMarkers(t)
			calls := fakeExec(t)
			_, err := runTq(t, "run", "acme", "--", "claude")
			if err == nil {
				t.Fatalf("a session started with %s", c.name)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("the refusal lacks %q: %v", w, err)
				}
			}
			if !strings.Contains(err.Error(), "\n  "+c.fix) {
				t.Errorf("the fix section is %q: %v", c.fix, err)
			}
			if len(*calls) != 0 {
				t.Fatalf("nothing may launch: %+v", *calls)
			}
		})
	}
}

// A plugin's PreToolUse is the guard only when it runs tq_hook.sh pre-tool-use with a matcher that
// covers Bash and PowerShell; anything else is no hook.
func TestRun_ForeignHookIsNoHook(t *testing.T) {
	hooks := func(matcher, command string) string {
		return fmt.Sprintf(`{"hooks":{"PreToolUse":[{"matcher":%q,"hooks":[{"type":"command","command":%q}]}]}}`, matcher, command)
	}
	guard := `bash "${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh" pre-tool-use`
	cases := []struct {
		name  string
		hooks string
		guard bool
	}{
		{"the release", "", true},
		{"another script", hooks("^(Bash|PowerShell|Read|Edit|Write|MultiEdit|NotebookEdit|Grep|Glob|Agent)$|^mcp__", `bash "${CLAUDE_PLUGIN_ROOT}/scripts/other.sh" pre-tool-use`), false},
		{"a script run by name", hooks("^(Bash|PowerShell)$|^mcp__", `node "${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.js" pre-tool-use`), false},
		{"the matcher leaves out Bash", hooks("^(PowerShell|Read|Edit|Write)$|^mcp__", guard), false},
		{"the matcher leaves out PowerShell", hooks("^(Bash|Read)$", guard), false},
		{"an invalid matcher", hooks("(Bash", guard), false},
		{"the hook is not a command", `{"hooks":{"PreToolUse":[{"matcher":"^(Bash|PowerShell)$","hooks":[{"type":"prompt","command":"` + guard + `"}]}]}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mkGuardWS(t, "acme", guardOpts{Hooks: c.hooks})
			clearAgentMarkers(t)
			calls := fakeExec(t)
			_, err := runTq(t, "run", "acme", "--", "claude")
			if c.guard != (err == nil) {
				t.Fatalf("the guard = %v, but the launch err = %v", c.guard, err)
			}
			if !c.guard && len(*calls) != 0 {
				t.Fatalf("nothing may launch: %+v", *calls)
			}
		})
	}
}

// Only a session needs the guard: the plugin's own commands, --version and --help run without one,
// so the fix line runs. Other commands through tentaqles run are not refused for it, and a neutral
// folder's claude is unchanged.
func TestRun_RefusalOnlyForASession(t *testing.T) {
	mkGuardWS(t, "acme", guardOpts{Absent: true, Identities: "{ claude: {}, gh: {} }"})
	clearAgentMarkers(t)
	calls := fakeExec(t)
	for _, args := range [][]string{
		{"run", "acme", "--", "claude", "plugin", "install", "x"},
		{"run", "acme", "--", "claude", "plugin", "install", "tentaqles@tentaqles"},
		{"run", "acme", "--", "claude", "--version"},
		{"run", "acme", "--", "claude", "--help"},
		{"run", "acme", "--", "gh", "pr", "list"},
	} {
		if out, err := runTq(t, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	if len(*calls) != 5 {
		t.Fatalf("calls %+v", *calls)
	}
	chdir(t, t.TempDir()) // neutral: no identity, no refusal
	t.Setenv("CLAUDE_CONFIG_DIR", "/other/client")
	calls = fakeExec(t)
	if out, err := runTq(t, "run", ".", "--", "claude", "-p", "hi"); err != nil {
		t.Fatalf("a neutral claude: %v %s", err, out)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls %+v", *calls)
	}
}

// The project's settings are merged into a session's, so a switch set off in the folders the session
// starts in refuses it, with the file named (REQ-8). A file above the client folder is not
// read, and a project file with neither switch starts.
func TestRun_ProjectSettingsCountToo(t *testing.T) {
	root, _ := mkGuardWS(t, "acme", guardOpts{})
	clearAgentMarkers(t)
	calls := fakeExec(t)
	chdir(t, root)
	project := filepath.Join(root, ".claude", "settings.local.json")
	for _, c := range []struct{ body, want string }{
		{`{"disableAllHooks":true}`, "disableAllHooks"},
		{`{"enabledPlugins":{"tentaqles@tentaqles":false}}`, "turned off"},
		{`{"disableAllHooks":`, "can't be read as JSON"},
	} {
		writeTB(t, project, c.body)
		_, err := runTq(t, "run", "acme", "--", "claude")
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), project) {
			t.Fatalf("%s: want the refusal naming the file, got %v", c.body, err)
		}
		// A file that is not JSON is repaired, then tentaqles run is tried again: the fix says so.
		if c.want == "can't be read as JSON" && !strings.Contains(err.Error(), "fix: repair "+project+" (it can't be read as the JSON it must be), then: tentaqles run acme -- claude") {
			t.Fatalf("a file that is not JSON gets the repair line: %v", err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing may launch: %+v", *calls)
	}
	// A project file with neither switch starts, and so does a folder under the root when it is the cwd.
	writeTB(t, project, `{"theme":"dark"}`)
	if _, err := runTq(t, "run", "acme", "--", "claude"); err != nil {
		t.Fatalf("a project with neither switch: %v", err)
	}
	// A project file in a folder the session starts in counts: the cwd below the root reads each folder up to it.
	app := filepath.Join(root, "app")
	writeTB(t, filepath.Join(app, ".claude", "settings.json"), `{"disableAllHooks":true}`)
	chdir(t, app)
	if _, err := runTq(t, "run", ".", "--", "claude"); err == nil || !strings.Contains(err.Error(), filepath.Join(app, ".claude", "settings.json")) {
		t.Fatalf("a switch in the cwd: %v", err)
	}
	// A file above the client folder is not one of the session's.
	if err := os.Remove(filepath.Join(app, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	writeTB(t, filepath.Join(filepath.Dir(root), ".claude", "settings.json"), `{"disableAllHooks":true}`)
	if _, err := runTq(t, "run", ".", "--", "claude"); err != nil {
		t.Fatalf("a folder above the client is not read: %v", err)
	}
}

// The lines a refusal prints get a developer a guarded session when run in order: the marketplace
// add leaves the plugin missing, the install brings it, and then the session starts. The lines run
// through the real launcher (no execCommand fake), so the session really starts.
func TestRun_FixLinesRunInOrder(t *testing.T) {
	mkGuardWS(t, "acme", guardOpts{Absent: true})
	clearAgentMarkers(t)
	bin := fakeClaude(t, testutil.FakeSpec{CopyArg: "install", Copy: guardCopies(t, "acme")})
	if _, err := runTq(t, "run", "acme", "--", "claude"); err == nil || !strings.Contains(err.Error(), "\n  "+fixBlockOf(guardInstallFix)) {
		t.Fatalf("before any line: want the refusal with the two lines, got %v", err)
	}
	if out, err := runTq(t, strings.Fields(guardInstallFix[0])[1:]...); err != nil {
		t.Fatalf("line 1 %q: %v %s", guardInstallFix[0], err, out)
	}
	if _, err := runTq(t, "run", "acme", "--", "claude"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("after line 1 the plugin is still missing: %v", err)
	}
	if out, err := runTq(t, strings.Fields(guardInstallFix[1])[1:]...); err != nil {
		t.Fatalf("line 2 %q: %v %s", guardFix, err, out)
	}
	if _, err := runTq(t, "run", "acme", "--", "claude"); err != nil {
		t.Fatalf("after line 2 the session starts: %v", err)
	}
	got := testutil.ReadFakeLog(t, filepath.Join(bin, "claude.argv.log"))
	if n := len(got); n < 3 || strings.Join(got[0], " ") != "plugin marketplace add tentaqles/tentaqles" ||
		strings.Join(got[1], " ") != "plugin install tentaqles@tentaqles" || strings.Join(got[n-1], " ") != "--dangerously-skip-permissions" {
		t.Fatalf("claude calls in order: %v", got)
	}
}

// The guard's check is cheap: a few small files, no process (REQ-4). The benchmark runs the check
// on an installed client.
func BenchmarkGuardHookPresent(b *testing.B) {
	home := b.TempDir()
	b.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	b.Setenv("HOME", home)
	b.Setenv("USERPROFILE", home)
	writeGuard(b, "acme", guardOpts{})
	dir := paths.IdentityDir("acme", "claude")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if g := guardplugin.CheckGuardHook(dir); !g.OK {
			b.Fatal(g.Why)
		}
	}
}

// mkGuardWS writes client name with a claude identity (or g.Identities) and its Claude config as
// g says. It returns the folder and the manifest path.
func mkGuardWS(t *testing.T, name string, g guardOpts) (string, string) {
	t.Helper()
	root := writeClient(t, newBase(t), name, "git: { email: tester@example.com }\nidentities: "+g.identities()+"\n")
	writeGuard(t, name, g)
	return root, paths.ManifestPath(name)
}

// jsonPathLit is p as a JSON string literal.
func jsonPathLit(p string) string {
	b, _ := json.Marshal(p)
	return string(b)
}
