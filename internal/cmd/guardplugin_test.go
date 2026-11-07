package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// The guard of a session is the tentaqles plugin's PreToolUse hook (ADR-0018): tq run refuses
// to start claude in an identity's folder when the client's Claude config has no enabled
// plugin that runs it. These tests write the plugin the way claude plugin install leaves it.

// guardOpts is the client's Claude config as a test writes it, before the trust is signed. The
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
	// Identities is the manifest's identities: flow map; "" is claude and codex.
	Identities string
	// NoRegistration writes no installed_plugins.json: claude loads the highest cached version
	// instead (PluginRoot's second source), as the integrity tests of a new plugin version need.
	NoRegistration bool
}

func (g guardOpts) identities() string {
	if g.Identities == "" {
		return "{ claude: {}, codex: {} }"
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
// enabledPlugins. It writes nothing signed; callers sign after.
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
const guardFix = "tq run acme -- claude plugin install tentaqles@tentaqles"

func TestRun_StartsWithTheHook(t *testing.T) {
	mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	if g := trust.CheckGuardHook(paths.IdentityDir("acme", "claude")); !g.OK {
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

// No plugin, no session: tq run <ws> and tq run . refuse with the fix line, and nothing launches.
// The fix line itself runs (TestRun_RefusalOnlyForASession).
func TestRun_RefusesWithNoGuardHook(t *testing.T) {
	root, _ := mkLaunchWSGuard(t, "acme", "", false, guardOpts{Absent: true})
	clearAgentMarkers(t)
	calls := fakeExec(t)
	for _, args := range [][]string{{"run", "acme", "--", "claude"}, {"run", "acme", "--", "claude", "-p", "hi"}} {
		_, err := runTq(t, args...)
		if err == nil || !strings.Contains(err.Error(), "without the tentaqles guard") || !strings.Contains(err.Error(), "\n  fix: "+guardFix) {
			t.Fatalf("%v: want the refusal with the fix line, got %v", args, err)
		}
	}
	chdir(t, root)
	_, err := runTq(t, "run", ".", "--", "claude")
	if err == nil || !strings.Contains(err.Error(), "\n  fix: "+guardFix) {
		t.Fatalf("tq run . : want the refusal with the fix line, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing may launch without the guard: %+v", *calls)
	}
}

// A plugin in the identity's Claude dir does not count when the session would run with no Claude
// config: a client without a claude identity has no CLAUDE_CONFIG_DIR, so claude would read the
// user's own config, which holds no guard of tq's.
func TestRun_NoClaudeIdentityIsNoGuard(t *testing.T) {
	mkLaunchWSGuard(t, "acme", "", false, guardOpts{Identities: "{ gh: {} }"})
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
	}{
		{"enabledPlugins false", guardOpts{Disabled: true}, []string{"not enabled", "enabledPlugins"}},
		{"disableAllHooks", guardOpts{DisableAllHooks: true}, []string{"disableAllHooks", "settings.json", "turns off every hook"}},
		{"no PreToolUse", guardOpts{Hooks: `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"bash \"${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh\" session-start"}]}]}}`},
			[]string{"no PreToolUse hook"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mkLaunchWSGuard(t, "acme", "", false, c.g)
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
			if c.g.DisableAllHooks && !strings.Contains(err.Error(), "fix: remove \"disableAllHooks\"") {
				t.Errorf("the fix names the setting: %v", err)
			}
			if len(*calls) != 0 {
				t.Fatalf("nothing may launch: %+v", *calls)
			}
		})
	}
}

// A plugin's PreToolUse is the guard only when it runs tq_hook.sh pre-tool-use with a matcher that
// covers Bash and MCP tools; anything else is no hook.
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
		{"the matcher leaves out Bash", hooks("^(Read|Edit|Write)$|^mcp__", guard), false},
		{"the matcher leaves out MCP", hooks("^(Bash|PowerShell|Read)$", guard), false},
		{"an invalid matcher", hooks("(Bash", guard), false},
		{"the hook is not a command", `{"hooks":{"PreToolUse":[{"matcher":"^(Bash|mcp__.*)$","hooks":[{"type":"prompt","command":"` + guard + `"}]}]}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mkLaunchWSGuard(t, "acme", "", false, guardOpts{Hooks: c.hooks})
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
// so the fix line runs. Other commands through tq run are not refused for it, and a neutral
// folder's claude is unchanged.
func TestRun_RefusalOnlyForASession(t *testing.T) {
	mkLaunchWSGuard(t, "acme", "", false, guardOpts{Absent: true, Identities: "{ claude: {}, gh: {} }"})
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

// The plugin's registration and its hooks are the guard: a session cannot write the settings,
// the installed_plugins.json or the plugin's hooks.json to remove it (REQ-11).
func TestProtectedPaths_PluginRegistration(t *testing.T) {
	f := setupProtected(t, "")
	installed := filepath.Join(paths.IdentityDir("acme", "claude"), "plugins", "installed_plugins.json")
	for _, p := range []string{f.settings, installed, f.pluginHooks} {
		for _, tool := range []string{"Write", "Edit"} {
			input := pathIn("file_path", p)
			if tool == "Edit" {
				input = `{"file_path":` + jsonPathLit(p) + `,"old_string":"a","new_string":"b"}`
			}
			code, category, rule, errOut := hookDecision(t, f.root, tool, input)
			if code != 2 || rule != policy.ProtectedPathID || category != policy.CategorySelfProtection ||
				!strings.HasPrefix(errOut, "BLOCKED [self-protection/tq/protected-path]: ") {
				t.Fatalf("%s %s: want the protected-path deny, got code=%d rule=%q stderr=%q", tool, p, code, rule, errOut)
			}
		}
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
		if g := trust.CheckGuardHook(dir); !g.OK {
			b.Fatal(g.Why)
		}
	}
}
