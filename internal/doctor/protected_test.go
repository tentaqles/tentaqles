package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func TestManifestLayout_OldLayoutWarnsAndNamesTheMigration(t *testing.T) {
	w := &resolve.Workspace{Name: "acme", Root: `/b/acme`, ManifestPath: `/b/acme/.tentaqles.yaml`}
	fs := collect(func(add func(string, string, string, string, string)) { manifestLayout(w, add) })
	if len(fs) != 1 || fs[0].Level != "warn" || fs[0].Code != "manifest-in-root" || fs[0].Workspace != "acme" {
		t.Fatalf("%+v", fs)
	}
	if !strings.Contains(fs[0].Msg, paths.ManifestPath("acme")) || fs[0].Fix != "tq migrate manifests --apply" {
		t.Fatalf("%+v", fs[0])
	}
}

func TestManifestLayout_StaleRootWarns(t *testing.T) {
	w := &resolve.Workspace{Name: "acme", Protected: true, ManifestPath: paths.ManifestPath("acme"), StaleRoot: `/b/acme/.tentaqles.yaml`}
	fs := collect(func(add func(string, string, string, string, string)) { manifestLayout(w, add) })
	if len(fs) != 1 || fs[0].Level != "warn" || fs[0].Code != "manifest-root-stale" || !strings.Contains(fs[0].Msg, "/b/acme/.tentaqles.yaml") {
		t.Fatalf("%+v", fs)
	}
}

func TestManifestLayout_ProtectedIsSilent(t *testing.T) {
	w := &resolve.Workspace{Name: "acme", Protected: true, ManifestPath: paths.ManifestPath("acme")}
	if fs := collect(func(add func(string, string, string, string, string)) { manifestLayout(w, add) }); len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
}

// Run reports manifest-in-root for an old-layout workspace, trusted or not.
func TestRun_ManifestInRoot(t *testing.T) {
	isolatedHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "legacy")
	os.MkdirAll(root, 0o755)
	mp := filepath.Join(root, manifest.FileName)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: legacy\ngit: { email: a@b.c }\n"), 0o600)
	h, _ := trust.HashFile(mp)
	trust.Allow(h)
	// And a protected-layout one, which must stay silent.
	root2 := filepath.Join(base, "modern")
	os.MkdirAll(root2, 0o755)
	os.WriteFile(filepath.Join(root2, manifest.FileName), manifest.MarkerBytes("modern"), 0o644)
	pp := paths.ManifestPath("modern")
	os.MkdirAll(filepath.Dir(pp), 0o700)
	os.WriteFile(pp, []byte("schema: tentaqles-client-v2\nclient: modern\ngit: { email: a@b.c }\n"), 0o600)

	fs := Run(cfg, Deps{Env: func(string) (string, bool) { return "", false }, Cwd: t.TempDir(),
		RunGit: func(...string) (string, error) { return "", nil }, LookPath: func(string) (string, error) { return "", os.ErrNotExist }})
	var got []Finding
	for _, f := range fs {
		if f.Code == "manifest-in-root" {
			got = append(got, f)
		}
	}
	if len(got) != 1 || got[0].Workspace != "legacy" || got[0].Fix != "tq migrate manifests --apply" {
		t.Fatalf("%+v", got)
	}
}

func TestProtectedACL(t *testing.T) {
	isolatedHome(t)
	prev := protectGOOS
	t.Cleanup(func() { protectGOOS = prev })

	protectGOOS = "linux"
	if fs := collect(func(add func(string, string, string, string, string)) { protectedACL(Deps{}, add) }); len(fs) != 0 {
		t.Fatalf("Windows-only, got %+v", fs)
	}

	protectGOOS = "windows"
	fs := collect(func(add func(string, string, string, string, string)) { protectedACL(Deps{}, add) })
	if len(fs) != 1 || fs[0].Level != "warn" || fs[0].Code != "protected-acl" || !strings.Contains(fs[0].Fix, "tq migrate manifests --apply") {
		t.Fatalf("no record: %+v", fs)
	}

	os.MkdirAll(paths.Home(), 0o700)
	os.WriteFile(paths.ACLRecord(), []byte(`{"ok":false,"at":"2026-11-05T10:00:00Z","detail":"icacls failed: exit status 5"}`), 0o600)
	fs = collect(func(add func(string, string, string, string, string)) { protectedACL(Deps{}, add) })
	if len(fs) != 1 || fs[0].Level != "warn" || !strings.Contains(fs[0].Msg, "exit status 5") || !strings.Contains(fs[0].Fix, "icacls") {
		t.Fatalf("failed record: %+v", fs)
	}

	os.WriteFile(paths.ACLRecord(), []byte(`{"ok":true,"at":"2026-11-05T10:00:00Z","detail":"processed"}`), 0o600)
	fs = collect(func(add func(string, string, string, string, string)) { protectedACL(Deps{}, add) })
	if len(fs) != 1 || fs[0].Level != "ok" || fs[0].Code != "protected-acl" || !strings.Contains(fs[0].Msg, "2026-11-05") {
		t.Fatalf("ok record: %+v", fs)
	}
}
