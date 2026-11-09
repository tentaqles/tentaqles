package cmd

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// sandboxEnv isolates the process for a sandbox build: the three home variables point at w/home
// (the home itself is not created here), the git global variables are unset, and the codex hook
// program is a fixed path. It returns w.
func sandboxEnv(t *testing.T) string {
	t.Helper()
	isolateHome(t)
	w := testutil.TempDir(t)
	home := filepath.Join(w, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	unsetenv(t, "GIT_CONFIG_GLOBAL")
	unsetenv(t, "GIT_CONFIG_SYSTEM")
	t.Setenv("TQ_NO_BANNER", "1")
	prev := hookProgramPath
	hookProgramPath = func() string { return filepath.FromSlash("/opt/tq/tentaqles") }
	t.Cleanup(func() { hookProgramPath = prev })
	return w
}

// countTree is the number of files and folders under dir, not counting dir itself; 0 when dir does
// not exist.
func countTree(t *testing.T, dir string) int {
	t.Helper()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0
	}
	n := 0
	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// runSandbox runs tentaqles lobe sandbox <folder> and returns its output and error.
func runSandbox(t *testing.T, folder string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"lobe", "sandbox", folder})
	err := root.Execute()
	return out.String(), err
}

// assertRefused checks a refused sandbox: an error, and output that starts with SETUP FAIL.
func assertRefused(t *testing.T, out string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("the sandbox was not refused:\n%s", out)
	}
	if !strings.HasPrefix(out, "SETUP FAIL") {
		t.Fatalf("output does not start with SETUP FAIL: %q", out)
	}
}

func TestLobeSandbox_BuildsTheStandardPair(t *testing.T) {
	w := sandboxEnv(t)
	out, err := runSandbox(t, w)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out != "ok acme globex\n" {
		t.Fatalf("output = %q, want %q", out, "ok acme globex\n")
	}
	for _, name := range []string{"acme", "globex"} {
		raw, err := os.ReadFile(paths.ManifestPath(name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(string(raw), "lobe:\n  model: off\n") {
			t.Errorf("%s manifest does not end with the lobe mapping:\n%s", name, raw)
		}
		if _, err := manifest.Load(paths.ManifestPath(name)); err != nil {
			t.Errorf("%s manifest does not load: %v", name, err)
		}
	}
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"acme", "globex"} {
		app := filepath.Join(w, "clients", name, "app")
		if err := os.MkdirAll(app, 0o755); err != nil {
			t.Fatal(err)
		}
		if ws := resolve.Resolve(app, cfg).Workspace; ws == nil || ws.Name != name {
			t.Errorf("resolving %s gives %+v, want client %s", app, ws, name)
		}
		if _, ok := cfg.Folder(name); !ok {
			t.Errorf("no folder is recorded for %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.IdentityDir("acme", "codex"), "config.toml")); err != nil {
		t.Errorf("acme has no codex config.toml: %v", err)
	}
	if _, err := os.Stat(paths.IdentityDir("globex", "codex")); !os.IsNotExist(err) {
		t.Errorf("globex has a codex folder: %v", err)
	}
	claude := paths.IdentityDir("acme", "claude")
	if _, err := os.Stat(filepath.Join(claude, "plugins")); !os.IsNotExist(err) {
		t.Errorf("acme's Claude config has a plugins folder: %v", err)
	}
	if settings, err := os.ReadFile(filepath.Join(claude, "settings.json")); err == nil {
		if strings.Contains(string(settings), `"plugins"`) || strings.Contains(string(settings), "enabledPlugins") {
			t.Errorf("acme's Claude settings name a plugin:\n%s", settings)
		}
	}
	home := filepath.Join(w, "home")
	for _, f := range []string{".bashrc", ".zshrc", ".bash_profile", ".profile", "Documents"} {
		if _, err := os.Stat(filepath.Join(home, f)); !os.IsNotExist(err) {
			t.Errorf("the sandbox home holds %s: %v", f, err)
		}
	}
}

func TestLobeSandbox_RefusesARealHome(t *testing.T) {
	t.Run("the home already holds a client", func(t *testing.T) {
		w := sandboxEnv(t)
		if out, err := runSandbox(t, w); err != nil {
			t.Fatalf("first build: %v\n%s", err, out)
		}
		before := countTree(t, w)
		out, err := runSandbox(t, w)
		assertRefused(t, out, err)
		if after := countTree(t, w); after != before {
			t.Fatalf("the refused run wrote files: %d before, %d after", before, after)
		}
	})

	t.Run("HOME outside the folder", func(t *testing.T) {
		w := sandboxEnv(t)
		t.Setenv("HOME", testutil.TempDir(t))
		folder := filepath.Join(w, "x")
		out, err := runSandbox(t, folder)
		assertRefused(t, out, err)
		if _, err := os.Stat(folder); !os.IsNotExist(err) {
			t.Fatalf("the folder %s was created: %v", folder, err)
		}
	})

	t.Run("GIT_CONFIG_GLOBAL set", func(t *testing.T) {
		w := sandboxEnv(t)
		t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(w, "g"))
		out, err := runSandbox(t, w)
		assertRefused(t, out, err)
		if _, err := os.Stat(filepath.Join(w, "home")); !os.IsNotExist(err) {
			t.Fatalf("the home was created: %v", err)
		}
	})

	t.Run("USERPROFILE unset", func(t *testing.T) {
		w := sandboxEnv(t)
		unsetenv(t, "USERPROFILE")
		out, err := runSandbox(t, w)
		assertRefused(t, out, err)
		if _, err := os.Stat(filepath.Join(w, "home")); !os.IsNotExist(err) {
			t.Fatalf("the home was created: %v", err)
		}
	})

	t.Run("clients folder not empty", func(t *testing.T) {
		w := sandboxEnv(t)
		if err := os.MkdirAll(filepath.Join(w, "clients"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(w, "clients", "keep.txt"), []byte("mine\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := countTree(t, w)
		out, err := runSandbox(t, w)
		assertRefused(t, out, err)
		if after := countTree(t, w); after != before {
			t.Fatalf("the refused run wrote files: %d before, %d after", before, after)
		}
	})
}

func TestLobeSandbox_LobeIsTheLastKey(t *testing.T) {
	w := sandboxEnv(t)
	if out, err := runSandbox(t, w); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, name := range []string{"acme", "globex"} {
		raw, err := os.ReadFile(paths.ManifestPath(name))
		if err != nil {
			t.Fatal(err)
		}
		last := ""
		for _, line := range strings.Split(string(raw), "\n") {
			if line != "" && line[0] >= 'a' && line[0] <= 'z' {
				last = line
			}
		}
		if last != "lobe:" {
			t.Fatalf("%s: last top-level key is %q, want lobe:", name, last)
		}
		f, err := os.OpenFile(paths.ManifestPath(name), os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("  enforce: block\n"); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := manifest.Load(paths.ManifestPath(name)); err != nil {
			t.Fatalf("%s manifest does not load after the lobe key grows: %v", name, err)
		}
	}
}

func TestLobeSandbox_WorksInsideAnAgent(t *testing.T) {
	w := sandboxEnv(t)
	t.Setenv("CLAUDECODE", "1")
	out, err := runSandbox(t, w)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "ok acme globex") {
		t.Fatalf("output = %q, want ok acme globex", out)
	}
}

func TestLobeCommandTable(t *testing.T) {
	var names []string
	for _, c := range newLobeCmd().Commands() {
		names = append(names, c.Name())
	}
	sort.Strings(names)
	// Each later lobe feature adds its own row here.
	if want := []string{"sandbox", "selftest"}; !slices.Equal(names, want) {
		t.Fatalf("lobe subcommands = %v, want %v", names, want)
	}
}
