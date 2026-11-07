package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// findCode returns the first finding with code, and whether there is one.
func findCode(fs []Finding, code string) (Finding, bool) {
	for _, f := range fs {
		if f.Code == code {
			return f, true
		}
	}
	return Finding{}, false
}

// guardedClient writes a trusted client acme whose Claude config holds the tentaqles plugin as
// claude plugin install leaves it (enabled, the release's hooks), unless plugin is false.
func guardedClient(t *testing.T, plugin bool) *registry.Config {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(root, manifest.FileName)
	if err := os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@b }\nidentities: { claude: {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if plugin {
		writeGuardFixture(t, "acme")
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow("acme", h); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// writeGuardFixture is the plugin's folder, installed_plugins.json and enabledPlugins of a client.
func writeGuardFixture(t *testing.T, name string) {
	t.Helper()
	claude := paths.IdentityDir(name, "claude")
	plugin := filepath.Join(claude, "plugins", "cache", "tentaqles", "tentaqles", "0.16.0")
	hooks, err := os.ReadFile(filepath.Join("..", "..", "plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(plugin, ".claude-plugin", "plugin.json"), `{"name":"tentaqles","version":"0.16.0"}`)
	put(t, filepath.Join(plugin, "hooks", "hooks.json"), string(hooks))
	put(t, filepath.Join(plugin, "scripts", "tq_hook.sh"), "#!/usr/bin/env bash\nexit 0\n")
	put(t, filepath.Join(claude, "plugins", "installed_plugins.json"),
		fmt.Sprintf(`{"version":2,"plugins":{"tentaqles@tentaqles":[{"scope":"user","installPath":%q,"version":"0.16.0"}]}}`, plugin))
	put(t, filepath.Join(claude, "settings.json"), `{"enabledPlugins":{"tentaqles@tentaqles":true}}`)
}

func put(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A trusted client with no tentaqles plugin reports guard-hook-missing as an error with the fix
// line that installs it (REQ-7); with the plugin in place the finding is gone.
func TestDoctor_GuardHookMissing(t *testing.T) {
	cfg := guardedClient(t, false)
	fs := Run(cfg, deps(nil, t.TempDir(), nil))
	f, ok := findCode(fs, "guard-hook-missing")
	if !ok {
		t.Fatalf("no guard-hook-missing: %+v", fs)
	}
	if f.Level != "error" || f.Workspace != "acme" || f.Fix != "tq run acme -- claude plugin install tentaqles@tentaqles" {
		t.Fatalf("finding %+v", f)
	}
	if Exit(fs) != 1 {
		t.Fatal("a missing guard is an error")
	}

	cfg = guardedClient(t, true)
	if fs := Run(cfg, deps(nil, t.TempDir(), nil)); has(fs, "guard-hook-missing") {
		t.Fatalf("the plugin is installed and enabled, yet: %+v", fs)
	}

	// The client's own folder's project settings switch the guard off for a session in it: the same
	// refusal, naming the file (REQ-8).
	project := filepath.Join(cfg.Bases[0], "acme", ".claude", "settings.local.json")
	put(t, project, `{"disableAllHooks":true}`)
	fs = Run(cfg, deps(nil, t.TempDir(), nil))
	f, ok = findCode(fs, "guard-hook-missing")
	if !ok || !strings.Contains(f.Msg, project) || f.Fix != "remove \"disableAllHooks\" (or set it to false) in "+project+", then: tq run acme -- claude" {
		t.Fatalf("project switch: %+v", f)
	}
}
