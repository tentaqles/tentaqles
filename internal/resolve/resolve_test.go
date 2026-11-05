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
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// ---------------------------------------------------------------- ADR-0002
// The protected layout: a name-only marker in the identity root, the manifest
// under $TQ_HOME/identities/<name>/manifest.yaml.

// mkProtected writes a protected-layout workspace: the marker at the root and
// the manifest under paths.ManifestPath(name).
func mkProtected(t *testing.T, base, name string, trusted bool) (root, pp string) {
	t.Helper()
	root = filepath.Join(base, name)
	os.MkdirAll(filepath.Join(root, "repo", "src"), 0o755)
	if err := os.WriteFile(filepath.Join(root, manifest.FileName), manifest.MarkerBytes(name), 0o644); err != nil {
		t.Fatal(err)
	}
	pp = paths.ManifestPath(name)
	os.MkdirAll(filepath.Dir(pp), 0o700)
	if err := os.WriteFile(pp, []byte("schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: "+name+"@x.io }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if trusted {
		h, _ := trust.HashFile(pp)
		trust.Allow(h)
	}
	return root, pp
}

func TestResolve_MarkerResolvesToProtectedManifest(t *testing.T) {
	base, cfg := setup(t)
	root, pp := mkProtected(t, base, "acme", true)
	r := Resolve(filepath.Join(root, "repo", "src"), cfg)
	if r.Workspace == nil || r.Reason != "" {
		t.Fatalf("%+v", r)
	}
	w := r.Workspace
	if w.Name != "acme" || !registry.SamePath(w.Root, root) || !registry.SamePath(w.ManifestPath, pp) || !w.Protected || w.StaleRoot != "" {
		t.Fatalf("%+v", w)
	}
	if w.Manifest.Git.Email != "acme@x.io" {
		t.Fatalf("the guard must read the protected copy, got %+v", w.Manifest.Git)
	}
}

func TestResolve_MarkerClientMismatch_Neutral(t *testing.T) {
	base, cfg := setup(t)
	root, _ := mkProtected(t, base, "acme", true)
	// The marker says it is another workspace: a copied folder, or a marker an
	// agent rewrote to borrow another identity.
	if err := os.WriteFile(filepath.Join(root, manifest.FileName), manifest.MarkerBytes("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Resolve(root, cfg)
	if r.Workspace != nil || r.Untrusted != nil || !strings.Contains(r.Reason, `"other"`) || !strings.Contains(r.Reason, `"acme"`) {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_MarkerWithoutProtectedManifest_Neutral(t *testing.T) {
	base, cfg := setup(t)
	root, pp := mkProtected(t, base, "acme", true)
	if err := os.Remove(pp); err != nil {
		t.Fatal(err)
	}
	r := Resolve(root, cfg)
	if r.Workspace != nil || r.Untrusted != nil || !strings.Contains(r.Reason, pp) {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_OldLayoutStillResolves(t *testing.T) {
	base, cfg := setup(t)
	root := mkws(t, base, "legacy", true)
	r := Resolve(root, cfg)
	if r.Workspace == nil || r.Workspace.Protected || r.Workspace.StaleRoot != "" {
		t.Fatalf("%+v", r)
	}
	if !registry.SamePath(r.Workspace.ManifestPath, filepath.Join(root, manifest.FileName)) {
		t.Fatalf("%+v", r.Workspace)
	}
}

// Both files present: the protected copy is the one read, the root copy is
// reported for tq doctor and nothing else.
func TestResolve_BothFiles_ProtectedWinsAndRootIsStale(t *testing.T) {
	base, cfg := setup(t)
	root := mkws(t, base, "acme", false) // a full manifest at the root, untrusted
	pp := paths.ManifestPath("acme")
	os.MkdirAll(filepath.Dir(pp), 0o700)
	if err := os.WriteFile(pp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: protected@x.io }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(pp)
	trust.Allow(h)
	r := Resolve(root, cfg)
	if r.Workspace == nil {
		t.Fatalf("%+v", r)
	}
	w := r.Workspace
	if !w.Protected || !registry.SamePath(w.ManifestPath, pp) || w.Manifest.Git.Email != "protected@x.io" {
		t.Fatalf("%+v", w)
	}
	if !registry.SamePath(w.StaleRoot, filepath.Join(root, manifest.FileName)) {
		t.Fatalf("StaleRoot = %q", w.StaleRoot)
	}
}

// Trust is the hash of the protected manifest: editing it loses trust, editing
// the marker alone changes nothing about the hash.
func TestResolve_EditedProtectedManifestLosesTrust_MarkerEditDoesNot(t *testing.T) {
	base, cfg := setup(t)
	root, pp := mkProtected(t, base, "acme", true)
	before := Resolve(root, cfg)
	if before.Workspace == nil {
		t.Fatalf("%+v", before)
	}
	// A comment added to the marker: same name, same hash, still trusted.
	mp := filepath.Join(root, manifest.FileName)
	b, _ := os.ReadFile(mp)
	if err := os.WriteFile(mp, append(b, "# edited\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	after := Resolve(root, cfg)
	if after.Workspace == nil || after.Workspace.Hash != before.Workspace.Hash {
		t.Fatalf("a marker edit must not change the trust hash: %+v", after)
	}
	// An edit of the protected manifest drops the trust until tq allow.
	if err := os.WriteFile(pp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: other@x.io }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Resolve(root, cfg)
	if r.Workspace != nil || r.Untrusted == nil || !strings.HasPrefix(r.Reason, "untrusted") {
		t.Fatalf("an edited protected manifest must lose trust: %+v", r)
	}
}

func TestListWorkspaces_BothLayouts(t *testing.T) {
	base, cfg := setup(t)
	mkws(t, base, "legacy", true)
	mkProtected(t, base, "modern", true)
	// A marker that names the wrong folder is reported, not listed.
	bad, _ := mkProtected(t, base, "bad", true)
	os.WriteFile(filepath.Join(bad, manifest.FileName), manifest.MarkerBytes("elsewhere"), 0o644)
	ws, errs := ListWorkspaces(cfg)
	if len(ws) != 2 || ws[0].Name != "legacy" || ws[1].Name != "modern" {
		t.Fatalf("%+v %v", ws, errs)
	}
	if ws[0].Protected || !ws[1].Protected {
		t.Fatalf("%+v", ws)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "bad") {
		t.Fatalf("errs = %v", errs)
	}
}

func setup(t *testing.T) (base string, cfg *registry.Config) {
	t.Helper()
	t.Setenv("TQ_HOME", t.TempDir())
	base = testutil.TempDir(t)
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

// A hand-edited config.yaml can hold a base with a trailing separator (AddBase
// would have cleaned it). Resolution must still work and must never slice cwd
// with the raw, longer base length.
func TestResolve_BaseWithTrailingSeparatorInConfig(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
	cfg := &registry.Config{Bases: []string{base + string(filepath.Separator)}}
	root := mkws(t, base, "acme", true)

	r := Resolve(filepath.Join(root, "repo", "src"), cfg)
	if r.Workspace == nil || r.Workspace.Name != "acme" {
		t.Fatalf("%+v", r)
	}
	if !registry.SamePath(r.Workspace.Root, root) {
		t.Fatalf("root %q != %q", r.Workspace.Root, root)
	}
	// The base itself and a sibling outside it must not panic either.
	if r := Resolve(base, cfg); r.Workspace != nil || r.Reason != "at base root" {
		t.Fatalf("%+v", r)
	}
	if r := Resolve(t.TempDir(), cfg); r.Workspace != nil {
		t.Fatalf("%+v", r)
	}
}

func TestResolve_UntrustedPopulatesUntrustedWorkspace(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, manifest.FileName)
	if err := os.WriteFile(p, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@acme.com }\nidentities: { claude: {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Resolve(root, cfg)
	if r.Workspace != nil {
		t.Fatalf("untrusted must stay fail-closed: %+v", r.Workspace)
	}
	if r.Untrusted == nil || r.Untrusted.Name != "acme" || r.Untrusted.Manifest == nil {
		t.Fatalf("expected Untrusted to name the workspace, got %+v", r.Untrusted)
	}
	if !strings.HasPrefix(r.Reason, "untrusted") {
		t.Fatalf("reason %q", r.Reason)
	}
}
