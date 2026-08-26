package resolve

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

func setup(t *testing.T) (base string, cfg *registry.Config) {
	t.Helper()
	t.Setenv("TQ_HOME", t.TempDir())
	base = t.TempDir()
	cfg = &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	return base, cfg
}

func mkws(t *testing.T, base, name string, trusted bool) string {
	t.Helper()
	root := filepath.Join(base, name)
	os.MkdirAll(filepath.Join(root, "repo", "src"), 0o755)
	p := filepath.Join(root, manifest.FileName)
	os.WriteFile(p, []byte("schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: "+name+"@x.io }\n"), 0o600)
	if trusted {
		h, _ := trust.HashFile(p)
		trust.Allow(h)
	}
	return root
}

func TestResolve_InsideTrustedWorkspace(t *testing.T) {
	base, cfg := setup(t)
	root := mkws(t, base, "acme", true)
	r := Resolve(filepath.Join(root, "repo", "src"), cfg)
	if r.Workspace == nil || r.Workspace.Name != "acme" || r.Reason != "" {
		t.Fatalf("%+v", r)
	}
	if !registry.SamePath(r.Workspace.Root, root) || r.Workspace.Manifest.Git.Email != "acme@x.io" {
		t.Fatalf("%+v", r.Workspace)
	}
}

func TestResolve_AtBase_Neutral(t *testing.T) {
	base, cfg := setup(t)
	if r := Resolve(base, cfg); r.Workspace != nil || r.Reason != "at base root" {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_OutsideBase_Neutral(t *testing.T) {
	_, cfg := setup(t)
	if r := Resolve(t.TempDir(), cfg); r.Workspace != nil || r.Reason != "outside any base" {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_NoManifest_Neutral(t *testing.T) {
	base, cfg := setup(t)
	d := filepath.Join(base, "newclient", "x")
	os.MkdirAll(d, 0o755)
	if r := Resolve(d, cfg); r.Workspace != nil || r.Reason != "no manifest" {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_Untrusted_Neutral(t *testing.T) {
	base, cfg := setup(t)
	root := mkws(t, base, "evil", false)
	r := Resolve(root, cfg)
	if r.Workspace != nil || !strings.HasPrefix(r.Reason, "untrusted") {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_EditedManifest_BecomesUntrusted(t *testing.T) {
	base, cfg := setup(t)
	root := mkws(t, base, "acme", true)
	os.WriteFile(filepath.Join(root, manifest.FileName), []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: other@x.io }\n"), 0o600)
	if r := Resolve(root, cfg); r.Workspace != nil {
		t.Fatal("edited manifest must lose trust")
	}
}

func TestResolve_PrefixCollision(t *testing.T) {
	base, cfg := setup(t)
	mkws(t, base, "acme", true)
	root2 := mkws(t, base, "acme2", true)
	if r := Resolve(root2, cfg); r.Workspace == nil || r.Workspace.Name != "acme2" {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_InvalidManifest_Neutral(t *testing.T) {
	base, cfg := setup(t)
	root := filepath.Join(base, "bad")
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, manifest.FileName), []byte("schema: nope\n"), 0o600)
	if r := Resolve(root, cfg); r.Workspace != nil || !strings.HasPrefix(r.Reason, "manifest invalid") {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_CaseInsensitiveOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	base, cfg := setup(t)
	root := mkws(t, base, "acme", true)
	if r := Resolve(strings.ToUpper(root), cfg); r.Workspace == nil {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_PreservesOnDiskCasing(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	base, cfg := setup(t)
	root := mkws(t, base, "Acme", true)
	r := Resolve(strings.ToLower(root), cfg)
	if r.Workspace == nil || r.Workspace.Name != "Acme" || filepath.Base(r.Workspace.Root) != "Acme" {
		t.Fatalf("%+v", r)
	}
}

func TestListWorkspaces(t *testing.T) {
	base, cfg := setup(t)
	mkws(t, base, "a", true)
	mkws(t, base, "b", false)
	os.MkdirAll(filepath.Join(base, "plain"), 0o755)
	ws, errs := ListWorkspaces(cfg)
	if len(errs) != 0 || len(ws) != 2 || ws[0].Name != "a" || ws[1].Name != "b" {
		t.Fatalf("%+v %v", ws, errs)
	}
}
