package guardplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		writeFile(t, p, body)
	}
	return claude
}

// resolvedTemp is the test's temp folder with its links resolved, as the check sees a folder.
func resolvedTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const guardHooks = `{"hooks":{"PreToolUse":[{"matcher":"^(Bash|PowerShell)$","hooks":[{"type":"command","command":"bash \"${CLAUDE_PLUGIN_ROOT}/scripts/tq_hook.sh\" pre-tool-use"}]}]}}`

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
	cases := map[string]struct{ settings, hooks string }{
		"settings not JSON":           {`{"enabledPlugins":`, guardHooks},
		"disableAllHooks as a string": {`{"enabledPlugins":{"tentaqles@tentaqles":true},"disableAllHooks":"true"}`, guardHooks},
		"hooks not JSON":              {`{"enabledPlugins":{"tentaqles@tentaqles":true}}`, `{"hooks":`},
		"enabled as a string":         {`{"enabledPlugins":{"tentaqles@tentaqles":"true"}}`, guardHooks},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if g := CheckGuardHook(guardClaudeDir(t, c.hooks, c.settings)); g.OK || g.Why == "" {
				t.Fatalf("want no guard with a reason, got %+v", g)
			}
		})
	}
	// disableAllHooks false is not a switch; the setting is named when it is set.
	if g := CheckGuardHook(guardClaudeDir(t, guardHooks, `{"enabledPlugins":{"tentaqles@tentaqles":true},"disableAllHooks":false}`)); !g.OK {
		t.Fatalf("disableAllHooks false: %+v", g)
	}
	if g := CheckGuardHook(guardClaudeDir(t, guardHooks, `{"enabledPlugins":{"tentaqles@tentaqles":true},"disableAllHooks":true}`)); g.OK || g.Setting != "disableAllHooks" {
		t.Fatalf("disableAllHooks true names the setting: %+v", g)
	}
	// No installed plugin at all: missing, with the three install lines.
	if g := CheckGuardHook(t.TempDir()); g.OK || g.Why == "" || !g.Missing {
		t.Fatalf("no plugin: %+v", g)
	}
	// No Claude config dir at all (a client with no claude identity): never the working directory's
	// settings.json, which would be read for ""; the fix names the manifest.
	if g := CheckGuardHook(""); g.OK || !g.NoConfig {
		t.Fatalf("no config dir: %+v", g)
	}
	if got := GuardHookFix("acme", GuardHook{NoConfig: true}); len(got) != 1 || got[0] != "add claude to the identities in acme's manifest, then: tentaqles settings render acme" {
		t.Fatalf("no-config fix = %q", got)
	}
}

// A project's settings are merged into the session's, so a switch set off there turns the guard off
// (REQ-8). Each file of each folder the session starts in is read; a file that is not JSON counts as
// off and is named; a file with neither switch does not.
func TestGuardHook_ProjectSettings(t *testing.T) {
	const ok = `{"enabledPlugins":{"tentaqles@tentaqles":true}}`
	cases := []struct {
		name    string
		file    string
		body    string
		wantOK  bool
		setting string
	}{
		{"no project file", "", "", true, ""},
		{"neither switch", ".claude/settings.json", `{"theme":"dark","enabledPlugins":{"other@x":false}}`, true, ""},
		{"disableAllHooks false", ".claude/settings.json", `{"disableAllHooks":false}`, true, ""},
		{"the tentaqles entry on", ".claude/settings.local.json", `{"enabledPlugins":{"tentaqles@tentaqles":true}}`, true, ""},
		{"disableAllHooks in settings.json", ".claude/settings.json", `{"disableAllHooks":true}`, false, "disableAllHooks"},
		{"disableAllHooks in settings.local.json", ".claude/settings.local.json", `{"disableAllHooks":true}`, false, "disableAllHooks"},
		{"the tentaqles entry off", ".claude/settings.local.json", `{"enabledPlugins":{"tentaqles@tentaqles":false}}`, false, "enabledPlugins"},
		{"the tentaqles entry as a string", ".claude/settings.json", `{"enabledPlugins":{"tentaqles@tentaqles":"true"}}`, false, "enabledPlugins"},
		{"enabledPlugins not an object", ".claude/settings.json", `{"enabledPlugins":true}`, false, ""},
		{"not JSON", ".claude/settings.json", `{"disableAllHooks":`, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			claude := guardClaudeDir(t, guardHooks, ok)
			root := resolvedTemp(t)
			if c.file != "" {
				writeFile(t, filepath.Join(root, c.file), c.body)
			}
			g := CheckSessionGuard(claude, root, root)
			if g.OK != c.wantOK {
				t.Fatalf("OK = %v, want %v: %+v", g.OK, c.wantOK, g)
			}
			if c.wantOK {
				return
			}
			if g.Why == "" || g.Setting != c.setting {
				t.Fatalf("the reason names the switch, and is not the identity's file: %+v", g)
			}
			if c.file != "" && g.File != filepath.Join(root, c.file) {
				t.Fatalf("File = %q, want the project file", g.File)
			}
		})
	}
}

// A project file is read only for a folder the session starts in: a sibling is not, and the folder
// above the client root is not, while the folders between cwd and root are.
func TestGuardHook_SessionDirs(t *testing.T) {
	base := resolvedTemp(t)
	root := filepath.Join(base, "acme")
	app := filepath.Join(root, "services", "api")
	for _, d := range []string{app, filepath.Join(root, "other")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, cwd, root string
		want            []string
	}{
		{"at the root", root, root, []string{root}},
		{"two levels down", app, root, []string{app, filepath.Join(root, "services"), root}},
		{"outside the root", base, root, []string{base}},
		{"no root", app, "", []string{app}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SessionDirs(c.cwd, c.root)
			if err != nil || strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("SessionDirs = %q, %v; want %q", got, err, c.want)
			}
		})
	}
	// A sibling's settings and the folder above the root are not read by a session in app.
	claude := guardClaudeDir(t, guardHooks, `{"enabledPlugins":{"tentaqles@tentaqles":true}}`)
	writeFile(t, filepath.Join(root, "other", ".claude", "settings.json"), `{"disableAllHooks":true}`)
	writeFile(t, filepath.Join(base, ".claude", "settings.json"), `{"disableAllHooks":true}`)
	if g := CheckSessionGuard(claude, app, root); !g.OK {
		t.Fatalf("a folder the session does not start in is not read: %+v", g)
	}
}

// S-1: a working folder reached through a link (a junction on Windows, a symlink elsewhere) is
// resolved before the walk, so the client's own project settings are still read. Not skipped on
// any OS: a link that cannot be made fails the test.
func TestGuardHook_LinkedFolder(t *testing.T) {
	base := resolvedTemp(t)
	root := filepath.Join(base, "acme")
	services := filepath.Join(root, "services")
	if err := os.MkdirAll(filepath.Join(services, "api"), 0o700); err != nil {
		t.Fatal(err)
	}
	// The project settings sit at the client's root; the session starts in a link to services/api.
	writeFile(t, filepath.Join(root, ".claude", "settings.json"), `{"disableAllHooks":true}`)
	link := filepath.Join(base, "link")
	if err := makeDirLink(services, link); err != nil {
		t.Fatalf("the link to %s could not be made: %v", services, err)
	}
	claude := guardClaudeDir(t, guardHooks, `{"enabledPlugins":{"tentaqles@tentaqles":true}}`)
	g := CheckSessionGuard(claude, filepath.Join(link, "api"), root)
	if g.OK || g.Setting != "disableAllHooks" || g.File != filepath.Join(root, ".claude", "settings.json") {
		t.Fatalf("a session started through a link reads the client's settings: %+v", g)
	}
}

// L-06: a client root reached through a real junction (a link, on any OS that has one) still has its
// project settings read: the link is followed, not taken for a folder, so a disableAllHooks in
// .claude/settings.local.json of the real folder refuses the session. Not skipped on Windows.
func TestGuardHook_JunctionedClientRootRefuses(t *testing.T) {
	base := resolvedTemp(t)
	real := filepath.Join(base, "clients", "acme")
	app := filepath.Join(real, "app")
	if err := os.MkdirAll(app, 0o700); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(real, ".claude", "settings.local.json")
	writeFile(t, project, `{"disableAllHooks":true}`)
	root := filepath.Join(base, "acme-link")
	if err := makeDirLink(real, root); err != nil {
		t.Fatalf("the junction to %s could not be made: %v", real, err)
	}
	claude := guardClaudeDir(t, guardHooks, `{"enabledPlugins":{"tentaqles@tentaqles":true}}`)
	for name, cwd := range map[string]string{"at the root": root, "in a folder under it": filepath.Join(root, "app")} {
		g := CheckSessionGuard(claude, cwd, root)
		if g.OK || g.Unresolved || g.Setting != "disableAllHooks" || g.File != project {
			t.Fatalf("%s: a junctioned client root refuses on its project settings, naming the real file: %+v", name, g)
		}
	}
}

// S-1: a working folder that cannot be resolved is refused, with the fix that runs it from a folder
// that resolves; a root that cannot be resolved is refused the same way.
func TestGuardHook_UnresolvedFolder(t *testing.T) {
	base := resolvedTemp(t)
	root := filepath.Join(base, "acme")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	claude := guardClaudeDir(t, guardHooks, `{"enabledPlugins":{"tentaqles@tentaqles":true}}`)
	for name, cwd := range map[string]string{"empty": "", "missing": filepath.Join(root, "gone")} {
		g := CheckSessionGuard(claude, cwd, root)
		if g.OK || !g.Unresolved || g.Why == "" {
			t.Fatalf("%s working folder: want a refusal, got %+v", name, g)
		}
		if got := GuardHookFix("acme", g); len(got) != 1 || !strings.Contains(got[0], "tentaqles run acme -- claude") {
			t.Fatalf("%s working folder: fix = %q", name, got)
		}
	}
	if g := CheckSessionGuard(claude, root, filepath.Join(base, "gone")); g.OK || !g.Unresolved {
		t.Fatalf("a root that does not resolve is refused: %+v", g)
	}
}

// The fix line names the file that holds the switch: a re-sign for the identity's settings (the
// record covers them), a new session for a project's.
func TestGuardHook_ProjectFixLines(t *testing.T) {
	got := GuardHookFix("acme", GuardHook{Why: "x", Setting: "disableAllHooks", File: "/p/.claude/settings.local.json"})
	if len(got) != 1 || got[0] != `remove "disableAllHooks" (or set it to false) in /p/.claude/settings.local.json, then: tentaqles run acme -- claude` {
		t.Fatalf("project fix = %q", got)
	}
	got = GuardHookFix("acme", GuardHook{Why: "x", Setting: "enabledPlugins", File: "/p/.claude/settings.json"})
	if len(got) != 1 || got[0] != `set "tentaqles@tentaqles" to true under enabledPlugins in /p/.claude/settings.json, then: tentaqles run acme -- claude` {
		t.Fatalf("enabledPlugins fix = %q", got)
	}
}

// The fix of a missing guard is the two install lines in order; a plugin that is installed and
// switched off is enabled; for disableAllHooks the fix names the setting and the file.
func TestGuardHook_FixLines(t *testing.T) {
	want := []string{
		"tentaqles run acme -- claude plugin marketplace add tentaqles/tentaqles",
		"tentaqles run acme -- claude plugin install tentaqles@tentaqles",
	}
	if got := GuardHookFix("acme", GuardHook{Why: "x", Missing: true}); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("missing fix = %q", got)
	}
	got := GuardHookFix("acme", GuardHook{Why: "x", Disabled: true})
	if strings.Join(got, "\n") != "tentaqles run acme -- claude plugin enable tentaqles@tentaqles" {
		t.Fatalf("disabled fix = %q", got)
	}
	got = GuardHookFix("acme", GuardHook{Why: "x", Setting: "disableAllHooks", File: "/c/settings.json"})
	if len(got) != 1 || got[0] != `remove "disableAllHooks" (or set it to false) in /c/settings.json, then: tentaqles run acme -- claude` {
		t.Fatalf("setting fix = %q", got)
	}
}

// cloudFolder is a folder that Windows reports with a reparse tag of its own, as a OneDrive
// placeholder folder is: a folder (IsDir) and a reparse point (ModeIrregular) at once.
type cloudFolder struct{ os.FileInfo }

func (c cloudFolder) Mode() os.FileMode { return c.FileInfo.Mode() | os.ModeIrregular }

// L-02: a folder with another reparse tag is a folder, not a link: the walk goes through it, so the
// client's project settings above it are still read. The tag cannot be made in CI, so lstatPath
// presents the real folder as Windows presents the cloud one.
func TestGuardHook_ReparsePointIsAFolder(t *testing.T) {
	base := resolvedTemp(t)
	cloud := filepath.Join(base, "OneDrive", "clients", "acme")
	app := filepath.Join(cloud, "app")
	if err := os.MkdirAll(app, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cloud, ".claude", "settings.json"), `{"disableAllHooks":true}`)
	prev := lstatPath
	lstatPath = func(p string) (os.FileInfo, error) {
		fi, err := prev(p)
		if err == nil && (p == filepath.Join(base, "OneDrive") || p == cloud) {
			return cloudFolder{fi}, nil
		}
		return fi, err
	}
	t.Cleanup(func() { lstatPath = prev })
	claude := guardClaudeDir(t, guardHooks, `{"enabledPlugins":{"tentaqles@tentaqles":true}}`)
	if g := CheckSessionGuard(claude, app, cloud); g.Unresolved || g.OK || g.Setting != "disableAllHooks" {
		t.Fatalf("a cloud folder is walked as a folder, and the project settings above the session are read: %+v", g)
	}
}

// L-02: which entries count as links. A symlink, and a reparse point that is not a folder (a
// junction, a mount point), do; a folder does not, whatever its reparse tag.
func TestGuardHook_IsLink(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fdi, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	ffi, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		fi   os.FileInfo
		want bool
	}{
		"a folder":                       {fdi, false},
		"a file":                         {ffi, false},
		"a folder with a reparse tag":    {cloudFolder{fdi}, false},
		"a reparse point that isn't one": {irregularFile{ffi}, true},
	}
	for name, c := range cases {
		if got := isLink(c.fi); got != c.want {
			t.Errorf("isLink(%s) = %v, want %v", name, got, c.want)
		}
	}
}

// irregularFile is a reparse point that is not a folder (a junction or mount point, on Windows).
type irregularFile struct{ os.FileInfo }

func (i irregularFile) Mode() os.FileMode { return i.FileInfo.Mode() | os.ModeIrregular }

// R-1: every cause the fix can't name by install gets the fix for that cause, and the lines that
// follow it. A settings file that is not the JSON it must be, or can't be read, is repaired first; a
// plugin hooks file that is not the guard is replaced by an uninstall and a fresh install.
func TestGuardHook_BrokenFileFixLines(t *testing.T) {
	hooks := guardHooks
	ok := `{"enabledPlugins":{"tentaqles@tentaqles":true}}`
	t.Run("identity settings.json not JSON", func(t *testing.T) {
		claude := guardClaudeDir(t, hooks, `{"enabledPlugins":`)
		g := CheckGuardHook(claude)
		want := fmt.Sprintf("repair %s (it can't be read as the JSON it must be), then: tentaqles run acme -- claude", filepath.Join(claude, "settings.json"))
		if g.OK || g.File != filepath.Join(claude, "settings.json") || strings.Join(GuardHookFix("acme", g), "\n") != want {
			t.Fatalf("%+v -> %q", g, GuardHookFix("acme", g))
		}
	})
	t.Run("identity settings.json unreadable", func(t *testing.T) {
		claude := guardClaudeDir(t, hooks, ok)
		settings := filepath.Join(claude, "settings.json")
		if err := os.Remove(settings); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(settings, 0o700); err != nil {
			t.Fatal(err)
		}
		g := CheckGuardHook(claude)
		if g.OK || g.File != settings || len(GuardHookFix("acme", g)) != 1 || !strings.HasPrefix(GuardHookFix("acme", g)[0], "repair "+settings) {
			t.Fatalf("%+v -> %q", g, GuardHookFix("acme", g))
		}
	})
	t.Run("project settings.local.json trailing comma", func(t *testing.T) {
		claude := guardClaudeDir(t, hooks, ok)
		root := resolvedTemp(t)
		project := filepath.Join(root, ".claude", "settings.local.json")
		writeFile(t, project, `{"disableAllHooks":false,}`)
		g := CheckSessionGuard(claude, root, root)
		want := fmt.Sprintf("repair %s (it can't be read as the JSON it must be), then: tentaqles run acme -- claude", project)
		if g.OK || g.File != project || strings.Join(GuardHookFix("acme", g), "\n") != want {
			t.Fatalf("%+v -> %q", g, GuardHookFix("acme", g))
		}
	})
	t.Run("project enabledPlugins not an object", func(t *testing.T) {
		claude := guardClaudeDir(t, hooks, ok)
		root := resolvedTemp(t)
		project := filepath.Join(root, ".claude", "settings.json")
		writeFile(t, project, `{"enabledPlugins":true}`)
		g := CheckSessionGuard(claude, root, root)
		if g.OK || g.File != project || len(GuardHookFix("acme", g)) != 1 || !strings.HasPrefix(GuardHookFix("acme", g)[0], "repair "+project) {
			t.Fatalf("%+v -> %q", g, GuardHookFix("acme", g))
		}
	})
	t.Run("plugin hooks.json not the guard", func(t *testing.T) {
		claude := guardClaudeDir(t, `{"hooks":`, ok)
		g := CheckGuardHook(claude)
		want := []string{
			"tentaqles run acme -- claude plugin uninstall tentaqles@tentaqles",
			"tentaqles run acme -- claude plugin marketplace add tentaqles/tentaqles",
			"tentaqles run acme -- claude plugin install tentaqles@tentaqles",
		}
		if g.OK || !g.Reinstall || strings.Join(GuardHookFix("acme", g), "\n") != strings.Join(want, "\n") {
			t.Fatalf("%+v -> %q", g, GuardHookFix("acme", g))
		}
	})
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
