package resolve

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// A second folder that carries a client's name and a copy of its marker, under another
// registered clients folder, is not that client.
func TestResolve_SecondFolderWithAClientsName(t *testing.T) {
	_, root := fixture(t)
	one := filepath.Dir(root)
	two := t.TempDir()
	write(t, filepath.Join(two, "acme", manifest.FileName), string(manifest.MarkerBytes("acme")))
	if err := os.MkdirAll(filepath.Join(two, "acme", "app", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	orders := []struct {
		name  string
		bases []string
	}{
		{"one first", []string{one, two}},
		{"two first", []string{two, one}},
	}
	for _, o := range orders {
		t.Run(o.name, func(t *testing.T) {
			c := &registry.Config{}
			for _, b := range o.bases {
				if _, err := c.AddBase(b); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Bind("acme", root); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{root, filepath.Join(root, "app")} {
				r := Resolve(dir, c)
				if r.Workspace == nil || r.Workspace.Name != "acme" || norm(r.Workspace.Root) != norm(root) {
					t.Errorf("1 control: %s: %+v", dir, r)
				}
			}
			if r := Resolve(filepath.Join(two, "acme"), c); r.Workspace != nil || !r.Unreadable {
				t.Errorf("2 second folder resolves as a client: %+v", r)
			}
			if r := Resolve(filepath.Join(two, "acme", "app", "src"), c); r.Workspace != nil || !r.Unreadable {
				t.Errorf("3 subfolder of the second folder resolves as a client: %+v", r)
			}
			all, _ := ListWorkspaces(c)
			var roots []string
			for _, w := range all {
				if w.Name == "acme" {
					roots = append(roots, w.Root)
				}
			}
			if len(roots) != 1 || norm(roots[0]) != norm(root) {
				t.Errorf("4 ListWorkspaces: found %d workspaces named acme, want 1 at %s; roots: %v", len(roots), root, roots)
			}
			if w, err := Find(c, "acme"); err != nil || w == nil || norm(w.Root) != norm(root) {
				t.Errorf("5 Find: err %v, workspace %+v, want root %s", err, w, root)
			}
			if _, errs := ListWorkspaces(c); len(errs) != 1 {
				t.Errorf("7 ListWorkspaces errors: got %d, want 1 (the copy): %v", len(errs), errs)
			}
			if runtime.GOOS == "windows" {
				if r := Resolve(filepath.Join(two, "Acme"), c); r.Workspace != nil || !r.Unreadable {
					t.Errorf("6 capital-A spelling of the second folder resolves as a client: %+v", r)
				}
			}
		})
	}
}

// A client created before the record existed has none; it resolves as before, unless a second
// folder claims its name.
func TestResolve_ClientWithNoRecordedFolder(t *testing.T) {
	cfg, root := fixture(t)
	if r := Resolve(root, cfg); r.Workspace == nil || r.Workspace.Name != "acme" || r.Unreadable {
		t.Errorf("1 no record, one folder: %+v", r)
	}
	two := t.TempDir()
	write(t, filepath.Join(two, "acme", manifest.FileName), string(manifest.MarkerBytes("acme")))
	if _, err := cfg.AddBase(two); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, filepath.Join(two, "acme")} {
		r := Resolve(dir, cfg)
		if r.Workspace != nil || !r.Unreadable || !strings.Contains(r.Reason, "tentaqles bind") {
			t.Errorf("2 two claims with no record, %s: %+v", dir, r)
		}
	}
	if all, errs := ListWorkspaces(cfg); len(all) != 0 || len(errs) != 2 {
		t.Errorf("2 ListWorkspaces: %d workspaces, %d errors; want 0 and 2: %+v %v", len(all), len(errs), all, errs)
	}
	if err := cfg.Bind("acme", root); err != nil {
		t.Fatal(err)
	}
	if r := Resolve(root, cfg); r.Workspace == nil {
		t.Errorf("3 bound to the folder: %+v", r)
	}
	if r := Resolve(filepath.Join(two, "acme"), cfg); r.Workspace != nil || !r.Unreadable {
		t.Errorf("3 the copy after bind: %+v", r)
	}
	if err := cfg.Bind("acme", filepath.Join(two, "acme")); err != nil {
		t.Fatal(err)
	}
	if r := Resolve(filepath.Join(two, "acme"), cfg); r.Workspace == nil || norm(r.Workspace.Root) != norm(filepath.Join(two, "acme")) {
		t.Errorf("4 the moved client: %+v", r)
	}
	if r := Resolve(root, cfg); r.Workspace != nil || !r.Unreadable {
		t.Errorf("4 the old folder after the move: %+v", r)
	}
}

// Claim finds the folder that claims a client, whatever the record says.
func TestResolve_Claim(t *testing.T) {
	cfg, root := fixture(t)
	two := t.TempDir()
	write(t, filepath.Join(two, "acme", manifest.FileName), string(manifest.MarkerBytes("acme")))
	if err := os.MkdirAll(filepath.Join(two, "acme", "app", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddBase(two); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Bind("acme", root); err != nil {
		t.Fatal(err)
	}
	name, r, err := Claim(filepath.Join(two, "acme", "app", "src"), cfg)
	if err != nil || name != "acme" || norm(r) != norm(filepath.Join(two, "acme")) {
		t.Errorf("1 Claim of the copy: name %q, root %s, err %v", name, r, err)
	}
	name, r, err = Claim(root, cfg)
	if err != nil || name != "acme" || norm(r) != norm(root) {
		t.Errorf("2 Claim of the recorded folder: name %q, root %s, err %v", name, r, err)
	}
	if _, _, err := Claim(t.TempDir(), cfg); err == nil {
		t.Errorf("3 Claim outside every base: no error")
	}
	one := filepath.Dir(root)
	if err := os.MkdirAll(filepath.Join(one, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Claim(filepath.Join(one, "plain"), cfg); err == nil {
		t.Errorf("4 Claim of a folder with no marker: no error")
	}
	write(t, filepath.Join(one, "other", manifest.FileName), string(manifest.MarkerBytes("acme")))
	if _, _, err := Claim(filepath.Join(one, "other"), cfg); err == nil {
		t.Errorf("5 Claim of a marker naming acme in folder other: no error")
	}
}
