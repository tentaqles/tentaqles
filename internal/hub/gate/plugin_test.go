package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindPluginRootReadsInstalledPlugins(t *testing.T) {
	t.Setenv("TQ_HUB_PLUGIN_ROOT", "")
	cfg := t.TempDir()
	install := filepath.Join(cfg, "plugins", "cache", "tentaqles", "tentaqles", "0.12.0")
	os.MkdirAll(filepath.Join(install, "scripts"), 0o755)
	os.WriteFile(filepath.Join(install, "scripts", "build-gate.py"), []byte("#"), 0o644)
	doc := `{"version":2,"plugins":{"other@x":[{"installPath":"/nope"}],"tentaqles@tentaqles":[{"installPath":` + quote(install) + `}]}}`
	os.WriteFile(filepath.Join(cfg, "plugins", "installed_plugins.json"), []byte(doc), 0o644)
	got, err := FindPluginRoot(cfg)
	if err != nil || got != install {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := FindPluginRoot(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing plugin: %v", err)
	}
	t.Setenv("TQ_HUB_PLUGIN_ROOT", install)
	if got, _ := FindPluginRoot(t.TempDir()); got != install {
		t.Fatal("TQ_HUB_PLUGIN_ROOT overrides")
	}
}

func TestForWorkspaceHonoursTheTestOverride(t *testing.T) {
	t.Setenv("TQ_HUB_GATE_CMD", `["python","fake_build_gate.py"]`)
	c, err := ForWorkspace(t.TempDir(), nil)
	if err != nil || len(c.Command) != 2 || c.Command[1] != "fake_build_gate.py" {
		t.Fatalf("%+v %v", c, err)
	}
	t.Setenv("TQ_HUB_GATE_CMD", "not json")
	if _, err := ForWorkspace(t.TempDir(), nil); err == nil {
		t.Fatal("a malformed override is an error")
	}
}

func quote(p string) string { return `"` + strings.ReplaceAll(p, `\`, `\\`) + `"` }
