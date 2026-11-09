package doctor

import (
	"os"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/tentaqles/tentaqles/internal/codexconfig"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// codexHome is the CODEX_HOME of client acme (the identity folder envplan gives it).
func codexHome(t *testing.T) string {
	t.Helper()
	return paths.IdentityDir("acme", "codex")
}

// editCodexConfig decodes acme's Codex config, applies edit, and writes it back.
func editCodexConfig(t *testing.T, edit func(root map[string]any)) {
	t.Helper()
	p := codexconfig.ConfigPath(codexHome(t))
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := toml.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	edit(root)
	out, err := toml.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	put(t, p, string(out))
}

// A client with codex in its scope reports the codex guard finding for each way its Codex config
// runs no hooks; a rendered config passes.
func TestDoctor_CodexGuard(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T)
		code  string
		fix   string
	}{
		{"no config", func(t *testing.T) {}, "codex-guard-missing", "tentaqles settings render acme"},
		{"hooks turned off under features", func(t *testing.T) {
			mustRenderCodex(t)
			editCodexConfig(t, func(root map[string]any) {
				root["features"] = map[string]any{"hooks": false}
			})
		}, "codex-guard-disabled", "tentaqles settings render acme"},
		{"state entry disabled", func(t *testing.T) {
			mustRenderCodex(t)
			editCodexConfig(t, func(root map[string]any) {
				hooks := root["hooks"].(map[string]any)
				for _, s := range hooks["state"].(map[string]any) {
					s.(map[string]any)["enabled"] = false
				}
			})
		}, "codex-guard-disabled", "tentaqles settings render acme"},
		{"not trusted", func(t *testing.T) {
			mustRenderCodex(t)
			editCodexConfig(t, func(root map[string]any) {
				delete(root["hooks"].(map[string]any), "state")
			})
		}, "codex-guard-untrusted", "tentaqles settings render acme"},
		{"config unreadable", func(t *testing.T) {
			put(t, codexconfig.ConfigPath(codexHome(t)), "hooks = [\n")
		}, "codex-guard-unreadable", "repair "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := trustedClient(t, "{ codex: {} }", false)
			tc.setup(t)
			fs := Run(cfg, deps(nil, t.TempDir(), nil))
			f, ok := findCode(fs, tc.code)
			if !ok {
				t.Fatalf("no %s: %+v", tc.code, fs)
			}
			if f.Level != "error" || f.Workspace != "acme" || !strings.Contains(f.Fix, tc.fix) {
				t.Fatalf("finding %+v", f)
			}
			if Exit(fs) != 1 {
				t.Fatal("a codex session with no guard is an error")
			}
		})
	}

	t.Run("rendered and trusted passes", func(t *testing.T) {
		cfg := trustedClient(t, "{ codex: {} }", false)
		mustRenderCodex(t)
		fs := Run(cfg, deps(nil, t.TempDir(), nil))
		for _, c := range []string{"codex-guard-missing", "codex-guard-disabled", "codex-guard-untrusted", "codex-guard-unreadable"} {
			if has(fs, c) {
				t.Fatalf("%s on a trusted client: %+v", c, fs)
			}
		}
	})

	t.Run("no codex in scope reports nothing", func(t *testing.T) {
		cfg := trustedClient(t, "{ gh: {} }", false)
		fs := Run(cfg, deps(nil, t.TempDir(), nil))
		for _, c := range []string{"codex-guard-missing", "codex-guard-disabled", "codex-guard-untrusted", "codex-guard-unreadable"} {
			if has(fs, c) {
				t.Fatalf("%s for a client without codex: %+v", c, fs)
			}
		}
	})
}

func mustRenderCodex(t *testing.T) {
	t.Helper()
	if _, err := codexconfig.Render(codexHome(t), codexconfig.Managed{Command: `"/opt/tq/tentaqles"`}); err != nil {
		t.Fatal(err)
	}
}
