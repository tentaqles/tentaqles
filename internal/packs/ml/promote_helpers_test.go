package ml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"gopkg.in/yaml.v3"
)

// TestMain doubles as the fake CLI (promotion and drift commands): with
// TQ_ML_FAKE=1 the test binary runs fakeMain instead of the tests.
func TestMain(m *testing.M) {
	if os.Getenv("TQ_ML_FAKE") == "1" {
		os.Exit(fakeMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeMain: --version | copy <src> <dst> | record <file> <args...> | exit <n>.
func fakeMain(args []string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "--version":
		os.Stdout.WriteString("fake 1.0.0\n")
		return 0
	case "copy":
		b, err := os.ReadFile(args[1])
		if err != nil {
			return 3
		}
		if err := os.MkdirAll(filepath.Dir(args[2]), 0o755); err != nil {
			return 3
		}
		if os.WriteFile(args[2], b, 0o644) != nil {
			return 3
		}
		return 0
	case "record":
		os.WriteFile(args[1], []byte(strings.Join(args[2:], "\n")), 0o644)
		os.Stdout.WriteString("promoted\n")
		if os.Getenv("TQ_ML_FAKE_EXIT") == "1" {
			return 1
		}
		return 0
	}
	return 2
}

// fakeCLI returns the path of the fake CLI and turns it on for children.
func fakeCLI(t *testing.T) string {
	t.Helper()
	t.Setenv("TQ_ML_FAKE", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(exe)
}

// isolate points TQ_HOME, HOME and USERPROFILE at a temp dir and clears the
// agent-session markers (the test itself may run inside an agent session).
func isolate(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TQ_HUB_TASK", "TENTAQLES_HEADLESS", "CLAUDECODE", "CLAUDE_PLUGIN_ROOT"} {
		t.Setenv(k, "")
	}
	return home
}

// configFrom builds a merged config from a packs: block (no validation, so
// keys owned by other ml workstreams can appear in fixtures).
func configFrom(t testing.TB, trusted, repo string) packs.Config {
	t.Helper()
	parse := func(src string) map[string]manifest.PackConfig {
		if src == "" {
			return nil
		}
		var m map[string]manifest.PackConfig
		if err := yaml.Unmarshal([]byte(src), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	return packs.Merge(parse(trusted), parse(repo))
}

// manifestFrom is a manifest whose packs: block is trusted (the layer
// tq-packs.yaml cannot loosen).
func manifestFrom(t testing.TB, trusted string) *manifest.Manifest {
	t.Helper()
	m := &manifest.Manifest{}
	if trusted == "" {
		return m
	}
	if err := yaml.Unmarshal([]byte(trusted), &m.Packs); err != nil {
		t.Fatal(err)
	}
	return m
}

// mlInput is an Input over root with trusted (the manifest) and repo
// (tq-packs.yaml) merged.
func mlInput(t testing.TB, root, trusted, repo string) *packs.Input {
	t.Helper()
	return &packs.Input{Root: root, Config: configFrom(t, trusted, repo), Manifest: manifestFrom(t, trusted)}
}

// setConfig rebinds in to new trusted and repo layers.
func setConfig(t testing.TB, in *packs.Input, trusted, repo string) {
	t.Helper()
	in.Config, in.Manifest = configFrom(t, trusted, repo), manifestFrom(t, trusted)
}

// writeFiles writes name -> content under root.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func rules(fs []packs.Finding, level string) []string {
	out := []string{}
	for _, f := range fs {
		if f.Level == level {
			out = append(out, f.Rule)
		}
	}
	return out
}
