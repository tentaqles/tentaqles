package trust

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// guardClaudeDir is a Claude config dir with the tentaqles plugin installed at the release's
// version, its hooks.json as hooks, and settings.json as settings. It returns the dir.
func guardClaudeDir(t *testing.T, hooks, settings string) string {
	t.Helper()
	claude := t.TempDir()
	plugin := filepath.Join(claude, "plugins", "cache", "tentaqles", "tentaqles", "0.16.0")
	files := map[string]string{
		filepath.Join(plugin, ".claude-plugin", "plugin.json"):     `{"name":"tentaqles","version":"0.16.0"}`,
		filepath.Join(plugin, "hooks", "hooks.json"):               hooks,
		filepath.Join(claude, "plugins", "installed_plugins.json"): fmt.Sprintf(`{"version":2,"plugins":{"tentaqles@tentaqles":[{"scope":"user","installPath":%q,"version":"0.16.0"}]}}`, plugin),
		filepath.Join(claude, "settings.json"):                     settings,
	}
	for p, body := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return claude
}

// The release's own hooks.json is a guard: the check accepts what ships.
func TestGuardHook_ShippedHooksPass(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if g := CheckGuardHook(guardClaudeDir(t, string(raw), `{"enabledPlugins":{"tentaqles@tentaqles":true}}`)); !g.OK {
		t.Fatalf("the shipped hooks are not a guard: %s", g.Why)
	}
}

func TestGuardHook_CommandForms(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{`bash "${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh" pre-tool-use`, true},
		{`bash ${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh pre-tool-use`, true},
		{`bash 'C:\plugins\tentaqles\scripts\tq_hook.sh' pre-tool-use`, true},
		{`sh ./scripts/tq_hook.sh pre-tool-use`, true},
		{`bash "${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh" session-start`, false},
		{`bash "${CLAUDE_PLUGIN_ROOT}/scripts/other.sh" pre-tool-use`, false},
		{`bash "${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh"`, false},
		{`bash "${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh" pre-tool-use extra`, false},
		{`node tq_hook.sh pre-tool-use`, false},
		{`bash -c "tq_hook.sh pre-tool-use"`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := runsGuardCommand(c.cmd); got != c.want {
			t.Errorf("runsGuardCommand(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestGuardHook_Matchers(t *testing.T) {
	cases := []struct {
		matcher string
		bash    bool
		mcp     bool
	}{
		{"", true, true},
		{"*", true, true},
		{"^(Bash|PowerShell|Read|Edit|Write|MultiEdit|NotebookEdit|Grep|Glob|Agent)$|^mcp__", true, true},
		{"Bash|mcp__.*", true, true},
		{"Bash", true, false},
		{"^mcp__", false, true},
		{"Read", false, false},
		{"(Bash", false, false},
	}
	for _, c := range cases {
		if got := matcherCovers(c.matcher, "Bash"); got != c.bash {
			t.Errorf("matcherCovers(%q, Bash) = %v", c.matcher, got)
		}
		if got := matcherCovers(c.matcher, "mcp__tentaqles__probe"); got != c.mcp {
			t.Errorf("matcherCovers(%q, mcp__...) = %v", c.matcher, got)
		}
	}
}

// Anything the check cannot read is no guard: it fails closed, with the reason.
func TestGuardHook_FailsClosed(t *testing.T) {
	hooks := `{"hooks":{"PreToolUse":[{"matcher":"^(Bash|mcp__.*)$","hooks":[{"type":"command","command":"bash \"${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh\" pre-tool-use"}]}]}}`
	cases := map[string]struct{ settings, hooks string }{
		"settings not JSON":           {`{"enabledPlugins":`, hooks},
		"disableAllHooks as a string": {`{"enabledPlugins":{"tentaqles@tentaqles":true},"disableAllHooks":"true"}`, hooks},
		"hooks not JSON":              {`{"enabledPlugins":{"tentaqles@tentaqles":true}}`, `{"hooks":`},
		"enabled as a string":         {`{"enabledPlugins":{"tentaqles@tentaqles":"true"}}`, hooks},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if g := CheckGuardHook(guardClaudeDir(t, c.hooks, c.settings)); g.OK || g.Why == "" {
				t.Fatalf("want no guard with a reason, got %+v", g)
			}
		})
	}
	// disableAllHooks false is not a switch; the setting is named when it is set.
	if g := CheckGuardHook(guardClaudeDir(t, hooks, `{"enabledPlugins":{"tentaqles@tentaqles":true},"disableAllHooks":false}`)); !g.OK {
		t.Fatalf("disableAllHooks false: %+v", g)
	}
	if g := CheckGuardHook(guardClaudeDir(t, hooks, `{"enabledPlugins":{"tentaqles@tentaqles":true},"disableAllHooks":true}`)); g.OK || g.Setting != "disableAllHooks" {
		t.Fatalf("disableAllHooks true names the setting: %+v", g)
	}
	// No installed plugin at all.
	if g := CheckGuardHook(t.TempDir()); g.OK || g.Why == "" {
		t.Fatalf("no plugin: %+v", g)
	}
	// No Claude config dir at all (a client with no claude identity): never the working directory's
	// settings.json, which would be read for ""; the fix names the manifest.
	if g := CheckGuardHook(""); g.OK || !g.NoConfig {
		t.Fatalf("no config dir: %+v", g)
	}
	if got := GuardHookFix("acme", GuardHook{NoConfig: true}); got != "add claude to the identities in acme's manifest, then: tq allow acme" {
		t.Fatalf("no-config fix = %q", got)
	}
}

// The fix line of a missing guard is the plugin install; for disableAllHooks it names the setting.
func TestGuardHook_FixLines(t *testing.T) {
	if got := GuardHookFix("acme", GuardHook{Why: "x"}); got != "tq run acme -- claude plugin install tentaqles@tentaqles" {
		t.Fatalf("plugin fix = %q", got)
	}
	got := GuardHookFix("acme", GuardHook{Why: "x", Setting: "disableAllHooks"})
	if got != `remove "disableAllHooks" (or set it to false) in the client's settings.json, then: tq hooks verify --accept acme` {
		t.Fatalf("setting fix = %q", got)
	}
}
