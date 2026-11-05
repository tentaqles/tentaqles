package protect

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fixtureSet is a protected set over a temp home: the state folder at
// <home>/.tentaqles, one registered base with workspace acme, Claude's
// config home at <home>/.claude, a tq binary at <home>/bin/tq.
func fixtureSet(t *testing.T) (*Set, string) {
	t.Helper()
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	state := filepath.Join(home, ".tentaqles")
	base := filepath.Join(home, "repos")
	for _, d := range []string{filepath.Join(state, "identities", "acme", "claude", "plugins", "cache", "tentaqles", "tentaqles", "1.0.0", "hooks"), filepath.Join(base, "acme"), filepath.Join(home, "bin")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"TQ_HOME": state, "CLAUDE_CONFIG_DIR": filepath.Join(state, "identities", "acme", "claude")}
	s := New(Inputs{
		Home: state, UserHome: home, Bases: []string{base}, Roots: []string{filepath.Join(base, "acme")},
		ConfigDirs: []string{filepath.Join(home, ".claude"), filepath.Join(state, "identities", "acme", "claude")},
		CodexDirs:  []string{filepath.Join(state, "identities", "acme", "codex")},
		Executable: filepath.Join(home, "bin", "tq"),
		Env:        func(k string) string { return env[k] },
	})
	return s, home
}

func TestNormalizeExpandsAndPlaces(t *testing.T) {
	s, home := fixtureSet(t)
	state := s.canon(filepath.Join(home, ".tentaqles"))
	cwd := filepath.Join(home, "repos", "acme")
	cases := map[string]string{
		"~/.tentaqles":                         state,
		"$HOME/.tentaqles/":                    state,
		"${HOME}/.tentaqles/config.yaml":       filepath.Join(state, "config.yaml"),
		"$TQ_HOME/trust":                       filepath.Join(state, "trust"),
		"$CLAUDE_CONFIG_DIR/settings.json":     filepath.Join(state, "identities", "acme", "claude", "settings.json"),
		`"` + home + `/.tentaqles"`:            state,
		"../../.tentaqles/config.yaml":         filepath.Join(state, "config.yaml"),
		".tentaqles.yaml":                      s.canon(filepath.Join(cwd, ".tentaqles.yaml")),
		"$HOMEBREW/.tentaqles":                 s.canon(filepath.Join(cwd, "$HOMEBREW", ".tentaqles")),
		filepath.ToSlash(home) + "/.tentaqles": state,
	}
	if runtime.GOOS == "windows" {
		cases[`$env:USERPROFILE\.tentaqles`] = state
		cases[`%USERPROFILE%\.tentaqles\trust`] = filepath.Join(state, "trust")
		cases[`$env:userprofile\.tentaqles`] = state
		vol := strings.ToLower(filepath.VolumeName(home))
		if len(vol) == 2 {
			msys := "/" + vol[:1] + filepath.ToSlash(home[2:]) + "/.tentaqles"
			cases[msys] = state
		}
	}
	for in, want := range cases {
		if got := s.Normalize(in, cwd); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
	if got := s.Normalize("config.yaml", ""); got != "" {
		t.Errorf("a relative path with no base is unplaceable, got %q", got)
	}
}

func TestMatchKinds(t *testing.T) {
	s, home := fixtureSet(t)
	state := filepath.Join(home, ".tentaqles")
	claude := filepath.Join(state, "identities", "acme", "claude")
	plugin := filepath.Join(claude, "plugins", "cache", "tentaqles", "tentaqles", "1.0.0")
	cases := []struct {
		p    string
		kind Kind // "" = not protected
	}{
		{state, KindState},
		{filepath.Join(state, "config.yaml"), KindState},
		{filepath.Join(state, "trust", "acme", "abc"), KindState},
		{filepath.Join(state, "identities", "acme", "manifest.yaml"), KindState},
		{filepath.Join(state, "identities", "acme"), KindState},
		{filepath.Join(state, "identities", "acme", "gh", "hosts.yml"), KindState},
		{filepath.Join(state, "denials.jsonl"), KindState},
		{filepath.Join(state, "anchors.yaml"), KindState},
		{filepath.Join(state, "integrity.key"), KindState},
		{filepath.Join(state, "bundles", "catalog.yaml"), KindState},
		{filepath.Join(state, "providers", "x.yaml"), KindState},
		{filepath.Join(state, "shims", "claude"), KindState},
		{claude, KindState},
		{filepath.Join(claude, "settings.json"), KindState},
		{filepath.Join(claude, "settings.local.json"), KindState},
		{filepath.Join(state, "identities", "acme", "codex", "hooks.json"), KindState},
		{filepath.Join(state, "identities", "acme", "codex", "config.toml"), KindState},
		{filepath.Join(claude, "plugins"), KindPlugin},
		{filepath.Join(claude, "plugins", "installed_plugins.json"), KindPlugin},
		{filepath.Join(claude, "plugins", "known_marketplaces.json"), KindPlugin},
		{filepath.Join(plugin, "hooks", "hooks.json"), KindPlugin},
		{filepath.Join(plugin, "scripts", "tq_hook.sh"), KindPlugin},
		{filepath.Join(home, ".claude", "plugins", "cache", "tentaqles", "tentaqles", "1.0.0", "hooks", "hooks.json"), KindPlugin},
		{filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), KindPlugin},
		// the tentaqles marketplace clone (P1.4's trust anchor), in the
		// default config home and in an identity's
		{filepath.Join(home, ".claude", "plugins", "marketplaces", "tentaqles"), KindPlugin},
		{filepath.Join(home, ".claude", "plugins", "marketplaces", "tentaqles", ".git", "config"), KindPlugin},
		{filepath.Join(claude, "plugins", "marketplaces", "tentaqles", "plugin", "hooks", "hooks.json"), KindPlugin},
		{filepath.Join(home, ".claude", "plugins", "marketplaces", "other", "x.json"), ""},
		// the plugin's pycache under the state folder
		{filepath.Join(state, "pycache", "x.cpython-312.pyc"), KindState},
		{filepath.Join(home, "repos", "acme", ".tentaqles.yaml"), KindMarker},
		{filepath.Join(home, "repos", "beta", ".tentaqles.yaml"), KindMarker}, // any workspace of a registered base
		{filepath.Join(home, "bin", "tq"), KindBinary},
		{filepath.Join(home, "bin"), KindBinary},
		{filepath.Join(home, "bin", "tq.exe"), KindBinary},
		{filepath.Join(home, "bin", "tq.old"), KindBinary},
		// not protected
		{filepath.Join(claude, "projects", "C--repos-acme", "memory", "MEMORY.md"), ""},
		{filepath.Join(claude, "todos", "x.json"), ""},
		{filepath.Join(claude, "CLAUDE.md"), ""},
		{filepath.Join(claude, "plugins", "cache", "other", "x.md"), ""},
		{filepath.Join(home, "bin", "other"), ""},
		{filepath.Join(home, "repos", "acme", "src", ".tentaqles.yaml"), ""}, // not a workspace root
		{filepath.Join(home, "repos", "acme", "notes.md"), ""},
		{filepath.Join(home, ".tentaqles-other", "x"), ""},
		{filepath.Join(home, ".claude", "settings.json"), ""},
	}
	for _, c := range cases {
		h, ok := s.Match(s.canon(c.p))
		switch {
		case c.kind == "" && ok:
			t.Errorf("%s: protected as %s (%s), want free", c.p, h.Kind, h.What)
		case c.kind != "" && (!ok || h.Kind != c.kind):
			t.Errorf("%s: got %v %q, want %s", c.p, ok, h.Kind, c.kind)
		}
	}
	for _, g := range []string{filepath.Join(state, "*"), filepath.Join(home, ".tentaqles*"), filepath.Join(home, "*")} {
		if _, ok := s.Match(s.canon(g)); !ok {
			t.Errorf("glob %s must match the state folder", g)
		}
	}
	if _, ok := s.Match(s.canon(filepath.Join(home, "other", "*"))); ok {
		t.Error("a glob outside every protected path matches nothing")
	}
	if h, _ := s.Match(s.canon(filepath.Join(state, "config.yaml"))); !h.ReadDenied() {
		t.Error("the state denies reads")
	}
	for _, p := range []string{filepath.Join(home, "repos", "acme", ".tentaqles.yaml"), filepath.Join(plugin, "hooks", "hooks.json"), filepath.Join(home, "bin", "tq")} {
		if h, _ := s.Match(s.canon(p)); h.ReadDenied() {
			t.Errorf("%s may be read", p)
		}
	}
}

// A bin folder of tentaqles' own is protected whole; a shared one only as
// itself and its tq-named files.
func TestBinaryDirectory(t *testing.T) {
	home := t.TempDir()
	owned := filepath.Join(home, "AppData", "Local", "tentaqles", "bin")
	s := New(Inputs{Executable: filepath.Join(owned, "tq.exe")})
	for _, p := range []string{filepath.Join(owned, "tq.exe"), owned, filepath.Join(owned, "helper.dll")} {
		if h, ok := s.Match(s.canon(p)); !ok || h.Kind != KindBinary {
			t.Errorf("%s: owned bin folder, got %v %q", p, ok, h.Kind)
		}
	}
	shared := filepath.Join(home, ".local", "bin")
	s = New(Inputs{Executable: filepath.Join(shared, "tq")})
	if _, ok := s.Match(s.canon(filepath.Join(shared, "jq"))); ok {
		t.Error("another tool in a shared bin folder is free")
	}
	for _, p := range []string{filepath.Join(shared, "tq"), shared, filepath.Join(shared, "tq.exe")} {
		if _, ok := s.Match(s.canon(p)); !ok {
			t.Errorf("%s: protected", p)
		}
	}
}

// A glob matches a protected path only if it could expand to one: it names
// it, or descends into a protected tree. `*.log` in a folder does not match
// `.tentaqles.yaml`; `.t*`, `*` and a glob descending into the state folder do.
func TestMatchGlob(t *testing.T) {
	s, home := fixtureSet(t)
	state := filepath.Join(home, ".tentaqles")
	root := filepath.Join(home, "repos", "acme")
	deny := []string{
		filepath.Join(root, ".t*"),                   // matches the marker
		filepath.Join(root, "*"),                     // matches the marker
		filepath.Join(home, ".tent*", "config.yaml"), // descends into the state folder
		filepath.Join(home, ".t?ntaqles", "config.yaml"),
		filepath.Join(state, "*"),            // inside the state folder
		filepath.Join(state, "**", "*.yaml"), // the Glob tool's recursive form
		filepath.Join(state, "trust", "*"),
	}
	allow := []string{
		filepath.Join(root, "*.log"), // cannot match .tentaqles.yaml
		filepath.Join(root, "*.md"),  //
		filepath.Join(root, "*.go"),  //
		filepath.Join(root, "*.bak"), //
		filepath.Join(root, "build", "*"),
		filepath.Join(root, "src", "*.tmp"),
		filepath.Join(home, "*.txt"),
	}
	for _, p := range deny {
		if _, ok := s.Match(s.Normalize(p, root)); !ok {
			t.Errorf("glob %q must be denied", p)
		}
	}
	for _, p := range allow {
		if h, ok := s.Match(s.Normalize(p, root)); ok {
			t.Errorf("glob %q must be allowed, matched %s", p, h.What)
		}
	}
}

// A glob under a workspace whose parent directory happens to hold the tq
// binary is not denied: the binary's directory is protected as itself and
// its tq files, not as a tree, so `rm *.log` there is free (the critic's
// fixture: tq.exe sits above the workspace).
func TestMatchGlobBinaryAncestorIsNotATree(t *testing.T) {
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	ws := filepath.Join(home, "scratch", "base", "acme") // under <scratch>, beside tq
	s := New(Inputs{
		Home: filepath.Join(home, ".tentaqles"), UserHome: home,
		Roots:      []string{ws},
		Executable: filepath.Join(home, "scratch", "tq.exe"), // an ancestor of ws
	})
	if h, ok := s.Match(s.Normalize("*.log", ws)); ok {
		t.Fatalf("a glob under the binary's ancestor dir must be free, matched %s (%s)", h.Path, h.Kind)
	}
	// Naming the binary itself with a glob still matches.
	if _, ok := s.Match(s.Normalize(filepath.Join(home, "scratch", "tq.*"), ws)); !ok {
		t.Fatal("a glob naming the tq binary is denied")
	}
}

// A recursive search rooted at an ancestor of the state folder reads it;
// one inside a registered base (repos kept under home) does not.
func TestSearchReadsState(t *testing.T) {
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	base := filepath.Join(home, "repos")
	s := New(Inputs{Home: filepath.Join(home, ".tentaqles"), UserHome: home, Bases: []string{base}})
	deny := []string{home, filepath.Dir(home)}
	allow := []string{
		base, filepath.Join(base, "acme"), // inside a base
		filepath.Join(home, ".tentaqles"), // the state folder itself (handled by the path rule, not an ancestor read)
		filepath.Join(home, "other"),      // a sibling that does not contain the state folder
	}
	for _, d := range deny {
		if !s.SearchReadsState(s.canon(d)) {
			t.Errorf("a search rooted at %q reaches the state folder", d)
		}
	}
	for _, d := range allow {
		if s.SearchReadsState(s.canon(d)) {
			t.Errorf("a search rooted at %q must not be an ancestor read", d)
		}
	}
}

// A destructive find inside a workspace whose parent directory holds tq is
// free (the binary directory is matched by name, not as a tree); a find on
// the whole home folder, or inside the state folder, is not.
func TestFindReachesSharedBinaryDir(t *testing.T) {
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	scratch := filepath.Join(home, "scratch")
	ws := filepath.Join(scratch, "base", "acme")
	s := New(Inputs{
		Home: filepath.Join(home, ".tentaqles"), UserHome: home,
		Roots: []string{ws}, Executable: filepath.Join(scratch, "tq.exe"),
	})
	if _, ok := s.FindReaches(s.canon(ws), []string{"*.pyc"}); ok {
		t.Fatal("a find inside a workspace under the binary's dir must be free")
	}
	if _, ok := s.FindReaches(s.canon(filepath.Join(home, ".tentaqles")), nil); !ok {
		t.Fatal("a find inside the state folder is denied")
	}
	if _, ok := s.FindReaches(s.canon(ws), []string{".tentaqles.yaml"}); !ok {
		t.Fatal("a find whose -name matches the workspace marker is denied")
	}
}

func TestMatchIsCaseInsensitiveWherePathsAre(t *testing.T) {
	if !caseInsensitivePaths() {
		t.Skip("case-sensitive paths on " + runtime.GOOS)
	}
	s, home := fixtureSet(t)
	p := strings.ToUpper(filepath.Join(home, ".tentaqles", "config.yaml"))
	if _, ok := s.Match(s.Normalize(p, "")); !ok {
		t.Fatalf("%s must match without case", p)
	}
}

func TestMatchFollowsSymlinks(t *testing.T) {
	s, home := fixtureSet(t)
	link := filepath.Join(home, "repos", "acme", "state-link")
	if err := os.Symlink(filepath.Join(home, ".tentaqles"), link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, ok := s.Match(s.Normalize(filepath.Join(link, "config.yaml"), "")); !ok {
		t.Fatal("a path through a link into the state folder is the state folder")
	}
}

func TestMentions(t *testing.T) {
	s, home := fixtureSet(t)
	state := filepath.ToSlash(filepath.Join(home, ".tentaqles"))
	for _, text := range []string{
		"open('" + state + "/config.yaml','w')",
		"require('fs').writeFileSync(\"" + state + "/identities/acme/manifest.yaml\")",
		"[IO.File]::WriteAllText('" + filepath.Join(home, ".tentaqles", "config.yaml") + "')",
		"x = '~/.tentaqles/trust'",
	} {
		if _, ok := s.Mentions(text); !ok {
			t.Errorf("%q names a protected path", text)
		}
	}
	for _, text := range []string{
		"open('" + state + "/identities/acme/claude/projects/x/memory/MEMORY.md')",
		"print('hello')",
		"x = '" + filepath.ToSlash(home) + "/repos/acme/notes.md'",
	} {
		if h, ok := s.Mentions(text); ok {
			t.Errorf("%q names nothing protected, got %s", text, h.Path)
		}
	}
}
