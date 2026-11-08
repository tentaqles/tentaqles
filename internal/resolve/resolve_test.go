package resolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
)

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fixture is a clients folder in a fresh TQ_HOME with client acme (marker and manifest).
func fixture(t *testing.T) (*registry.Config, string) {
	t.Helper()
	t.Setenv("TQ_HOME", t.TempDir())
	base := t.TempDir()
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "acme")
	write(t, filepath.Join(root, manifest.FileName), string(manifest.MarkerBytes("acme")))
	write(t, paths.ManifestPath("acme"), "schema: tentaqles-client-v2\nclient: acme\ngit: { email: dev@acme.example }\n")
	return cfg, root
}

// The client folder and every subfolder resolve to the client.
func TestResolve_FolderAndSubfolders(t *testing.T) {
	cfg, root := fixture(t)
	for _, dir := range []string{root, filepath.Join(root, "app", "src")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		r := Resolve(dir, cfg)
		if r.Workspace == nil || r.Workspace.Name != "acme" || r.Workspace.Manifest.Git.Email != "dev@acme.example" {
			t.Fatalf("%s: %+v", dir, r)
		}
	}
}

// Outside every clients folder, at the clients folder itself, and in a folder with no marker,
// nothing resolves; none of them is unreadable.
func TestResolve_Neutral(t *testing.T) {
	cfg, root := fixture(t)
	base := filepath.Dir(root)
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	for dir, reason := range map[string]string{t.TempDir(): "outside any base", base: "at base root", plain: "no marker"} {
		r := Resolve(dir, cfg)
		if r.Workspace != nil || r.Unreadable || r.Reason != reason {
			t.Errorf("%s: %+v, want reason %q", dir, r, reason)
		}
	}
}

// A marker that names another client, or a manifest whose client is another folder, lends the
// folder nothing: a copied marker or manifest is not that client.
func TestResolve_MarkerOrManifestOfAnotherClient(t *testing.T) {
	cfg, root := fixture(t)
	other := filepath.Join(filepath.Dir(root), "other")
	write(t, filepath.Join(other, manifest.FileName), string(manifest.MarkerBytes("acme")))
	if r := Resolve(other, cfg); r.Workspace != nil {
		t.Fatalf("a marker naming acme made other a client: %+v", r)
	}
	write(t, filepath.Join(other, manifest.FileName), string(manifest.MarkerBytes("other")))
	write(t, paths.ManifestPath("other"), "schema: tentaqles-client-v2\nclient: acme\n")
	if r := Resolve(other, cfg); r.Workspace != nil {
		t.Fatalf("acme's manifest made other a client: %+v", r)
	}
}

// A marker with no manifest, or a manifest that does not parse, is unreadable: the hook denies
// there.
func TestResolve_Unreadable(t *testing.T) {
	cfg, root := fixture(t)
	if err := os.Remove(paths.ManifestPath("acme")); err != nil {
		t.Fatal(err)
	}
	if r := Resolve(root, cfg); r.Workspace != nil || !r.Unreadable {
		t.Fatalf("marker without a manifest: %+v", r)
	}
	write(t, paths.ManifestPath("acme"), "schema: [")
	if r := Resolve(root, cfg); r.Workspace != nil || !r.Unreadable {
		t.Fatalf("a manifest that does not parse: %+v", r)
	}
}

func TestListAndFind(t *testing.T) {
	cfg, _ := fixture(t)
	all, errs := ListWorkspaces(cfg)
	if len(all) != 1 || all[0].Name != "acme" || len(errs) != 0 {
		t.Fatalf("%+v %v", all, errs)
	}
	if w, err := Find(cfg, "acme"); err != nil || w.Name != "acme" {
		t.Fatalf("%+v %v", w, err)
	}
	if _, err := Find(cfg, "ghost"); err == nil {
		t.Fatal("a client that does not exist was found")
	}
}
