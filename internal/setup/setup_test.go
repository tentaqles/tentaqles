package setup

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/detect"
	"github.com/tentaqles/tentaqles/cli/internal/hooks"
	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/providers"
)

func fakeGit(...string) (string, error) { return "", nil }

// isolateHome creates a temp dir and points TQ_HOME/HOME (or USERPROFILE on
// Windows) at it, mirroring internal/workspace/scaffold_test.go, so registry
// and gitcfg operations never touch the real developer home directory.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	return home
}

func tempProfiles(t *testing.T, shells ...hooks.Shell) hooks.Profiles {
	t.Helper()
	dir := t.TempDir()
	p := hooks.Profiles{}
	for _, sh := range shells {
		p[sh] = filepath.Join(dir, string(sh)+"-profile")
	}
	return p
}

func TestPlan_YAMLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.yaml")

	orig := Example()
	if err := orig.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Base != orig.Base || len(loaded.Companies) != len(orig.Companies) || loaded.Trust != orig.Trust {
		t.Fatalf("round trip mismatch: %+v vs %+v", loaded, orig)
	}
	if loaded.Companies[0].Name != orig.Companies[0].Name || loaded.Companies[0].GitEmail != orig.Companies[0].GitEmail {
		t.Fatalf("company round trip mismatch: %+v vs %+v", loaded.Companies[0], orig.Companies[0])
	}
}

func TestLoadPlan_TrustDefaultsTrueWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.yaml")
	yamlContent := "base: /repos\ncompanies:\n  - name: acme\n    git_name: A\n    git_email: a@acme.com\n"
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Trust {
		t.Fatal("expected Trust to default to true when absent from YAML")
	}
}

func testCatalog(t *testing.T) *providers.Catalog {
	t.Helper()
	cat, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func TestValidate_Errors(t *testing.T) {
	cat := testCatalog(t)
	base := func() *SetupPlan {
		return &SetupPlan{
			Base: "/repos",
			Companies: []Company{
				{Name: "acme", GitName: "A", GitEmail: "a@acme.com", Identities: []string{"claude", "gh"}, PermissionMode: "default"},
			},
		}
	}

	t.Run("bad name", func(t *testing.T) {
		p := base()
		p.Companies[0].Name = "Acme Corp!"
		if err := p.Validate(cat); err == nil {
			t.Fatal("expected error for bad name")
		}
	})

	t.Run("unknown identity", func(t *testing.T) {
		p := base()
		p.Companies[0].Identities = []string{"not-a-real-provider"}
		if err := p.Validate(cat); err == nil {
			t.Fatal("expected error for unknown identity")
		}
	})

	t.Run("bad permission mode", func(t *testing.T) {
		p := base()
		p.Companies[0].PermissionMode = "yolo"
		if err := p.Validate(cat); err == nil {
			t.Fatal("expected error for bad permission mode")
		}
	})

	t.Run("dup", func(t *testing.T) {
		p := base()
		p.Companies = append(p.Companies, p.Companies[0])
		if err := p.Validate(cat); err == nil {
			t.Fatal("expected error for duplicate company name")
		}
	})

	t.Run("valid", func(t *testing.T) {
		p := base()
		if err := p.Validate(cat); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestPreview_ReportsSkipsForExisting(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "acme"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "acme", manifest.FileName), []byte("schema: tentaqles-client-v2\nclient: acme\n"), 0644); err != nil {
		t.Fatal(err)
	}

	p := &SetupPlan{
		Base:  base,
		Trust: true,
		Hooks: []string{"bash"},
		Companies: []Company{
			{Name: "acme", GitName: "A", GitEmail: "a@acme.com"},
			{Name: "newco", GitName: "B", GitEmail: "b@newco.com"},
		},
	}
	hp := tempProfiles(t, "bash")

	changes, err := Preview(p, hp)
	if err != nil {
		t.Fatal(err)
	}

	kindsByTarget := map[string]string{}
	for _, c := range changes {
		if c.Target == "acme" || c.Target == "newco" || c.Target == "bash" {
			kindsByTarget[c.Kind+":"+c.Target] = c.Detail
		}
	}
	if _, ok := kindsByTarget["workspace-skip:acme"]; !ok {
		t.Errorf("expected workspace-skip for acme, got %+v", changes)
	}
	if _, ok := kindsByTarget["workspace-create:newco"]; !ok {
		t.Errorf("expected workspace-create for newco, got %+v", changes)
	}
	if _, ok := kindsByTarget["hook-install:bash"]; !ok {
		t.Errorf("expected hook-install for bash, got %+v", changes)
	}
}

func TestToolCheck_DedupsPerCompany(t *testing.T) {
	cat := testCatalog(t)
	p := &SetupPlan{
		Base: "/repos",
		Companies: []Company{
			{Name: "acme", GitName: "A", GitEmail: "a@acme.com", Identities: []string{"claude", "gh"}},
			{Name: "globex", GitName: "B", GitEmail: "b@globex.com", Identities: []string{"claude"}},
		},
	}

	calls := 0
	d := detect.Deps{
		LookPath: func(name string) (string, error) {
			calls++
			return "/usr/bin/" + name, nil
		},
		Run: func(ctx context.Context, name string, args ...string) (string, error) {
			return "v1.0.0", nil
		},
		GOOS: runtime.GOOS,
	}

	results := ToolCheck(p, cat, d)
	if len(results["acme"]) != 2 {
		t.Fatalf("expected 2 results for acme, got %d", len(results["acme"]))
	}
	if len(results["globex"]) != 1 {
		t.Fatalf("expected 1 result for globex, got %d", len(results["globex"]))
	}
	// claude is checked for both companies; gh only for acme. Both providers
	// have a CLI, so LookPath should be called exactly once per distinct id.
	if calls != 2 {
		t.Fatalf("expected LookPath called once per distinct identity (2), got %d", calls)
	}
}

func TestApply_EndToEnd(t *testing.T) {
	isolateHome(t)
	cat := testCatalog(t)
	base := t.TempDir()

	p := &SetupPlan{
		Base:  base,
		Trust: true,
		Hooks: []string{"bash"},
		Companies: []Company{
			{Name: "acme", GitName: "Jane", GitEmail: "jane@acme.com", Identities: []string{"claude", "gh"}, PermissionMode: "default"},
			{Name: "globex", GitName: "Jane", GitEmail: "jane@globex.com", Identities: []string{"claude", "gh"}, PermissionMode: "default"},
		},
	}
	hp := tempProfiles(t, "bash")

	report, err := Apply(p, cat, ApplyOptions{RunGit: fakeGit, Profiles: hp})
	if err != nil {
		t.Fatalf("unexpected error: %v; warnings=%v", err, report.Warnings)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", report.Warnings)
	}

	for _, name := range []string{"acme", "globex"} {
		mp := filepath.Join(base, name, manifest.FileName)
		if _, err := os.Stat(mp); err != nil {
			t.Fatalf("missing manifest for %s: %v", name, err)
		}
	}

	data, err := os.ReadFile(hp["bash"])
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected hooks profile to be written")
	}

	wantLogins := map[string]bool{
		"tq login acme claude":   true,
		"tq login acme gh":       true,
		"tq login globex claude": true,
		"tq login globex gh":     true,
	}
	for _, l := range report.Logins {
		delete(wantLogins, l)
	}
	if len(wantLogins) != 0 {
		t.Fatalf("missing expected logins: %v; got %v", wantLogins, report.Logins)
	}
}

func TestApply_ContinuesOnCompanyError(t *testing.T) {
	isolateHome(t)
	cat := testCatalog(t)
	base := t.TempDir()

	p := &SetupPlan{
		Base:  base,
		Trust: true,
		Companies: []Company{
			{Name: "badco", GitName: "Bad", GitEmail: "not-a-valid-email\x01", PermissionMode: "default"},
			{Name: "goodco", GitName: "Good", GitEmail: "good@goodco.com", PermissionMode: "default"},
		},
	}

	report, err := Apply(p, cat, ApplyOptions{RunGit: fakeGit})
	if err != nil {
		t.Fatalf("unexpected error (one company should still succeed): %v", err)
	}
	if len(report.Warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %v", report.Warnings)
	}
	if _, err := os.Stat(filepath.Join(base, "badco", manifest.FileName)); err == nil {
		t.Fatal("badco should not have been scaffolded")
	}
	if _, err := os.Stat(filepath.Join(base, "goodco", manifest.FileName)); err != nil {
		t.Fatalf("goodco should have been scaffolded: %v", err)
	}
}
