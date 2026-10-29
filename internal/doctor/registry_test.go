package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func TestRegisteredChecksRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(root, manifest.FileName)
	if err := os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: tester@example.com }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(mp)
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}

	var seenWS []string
	Register(Check{
		Name: "zz-test",
		Workspace: func(w *resolve.Workspace, _ Deps, add AddFunc) {
			seenWS = append(seenWS, w.Name)
			add("info", "zz-ws", w.Name, "ws", "")
		},
		Global: func(_ Deps, add AddFunc) { add("info", "zz-global", "", "global", "") },
	})
	t.Cleanup(func() { unregister("zz-test") })

	fs := Run(cfg, deps(nil, root, nil))
	if !has(fs, "zz-ws") || !has(fs, "zz-global") || len(seenWS) != 1 || seenWS[0] != "acme" {
		t.Fatalf("registered checks did not run: %v %+v", seenWS, fs)
	}
}

func TestRegisterPanicsOnDuplicateAndEmpty(t *testing.T) {
	Register(Check{Name: "zz-dup"})
	t.Cleanup(func() { unregister("zz-dup") })
	for _, c := range []Check{{Name: "zz-dup"}, {}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%q) must panic", c.Name)
				}
			}()
			Register(c)
		}()
	}
}
