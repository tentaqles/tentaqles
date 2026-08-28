package setupapi

import (
	"github.com/tentaqles/tentaqles/cli/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// isolateHome creates a temp dir and points TQ_HOME/HOME (or USERPROFILE on
// Windows) at it, mirroring internal/setup/setup_test.go, so registry and
// gitcfg operations never touch the real developer home directory.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	return home
}

func fakeGit(...string) (string, error) { return "", nil }

func tempProfiles(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	return map[string]string{
		"bash": filepath.Join(dir, "bash-profile"),
		"zsh":  filepath.Join(dir, "zsh-profile"),
	}
}

func TestProviders_NonEmpty(t *testing.T) {
	isolateHome(t)
	ps, err := Providers()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) == 0 {
		t.Fatal("expected at least one provider from the embedded catalog")
	}
	for _, p := range ps {
		if p.ID == "" || p.Name == "" || p.Category == "" {
			t.Fatalf("provider missing required fields: %+v", p)
		}
	}
}

func TestPreviewApply_RoundTrip(t *testing.T) {
	home := isolateHome(t)
	SetTestProfiles(tempProfiles(t))
	defer SetTestProfiles(nil)

	prevRunGit := RunGit
	RunGit = fakeGit
	defer func() { RunGit = prevRunGit }()

	base := filepath.Join(home, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}

	plan := Plan{
		Base:  base,
		Trust: true,
		Hooks: []string{"bash"},
		Companies: []Company{
			{
				Name:           "acme",
				GitName:        "Jane Doe",
				GitEmail:       "jane@acme.com",
				Identities:     []string{"claude", "gh"},
				PermissionMode: "acceptEdits",
			},
		},
	}

	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("ValidatePlan: %v", err)
	}

	changes, err := Preview(plan)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("expected preview changes")
	}
	foundCreate := false
	for _, c := range changes {
		if c.Kind == "workspace-create" && c.Target == "acme" {
			foundCreate = true
		}
	}
	if !foundCreate {
		t.Fatalf("expected a workspace-create change for acme, got %+v", changes)
	}

	report, err := Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	appliedCreate := false
	for _, c := range report.Changes {
		if c.Kind == "workspace-create" && c.Target == "acme" {
			appliedCreate = true
		}
	}
	if !appliedCreate {
		t.Fatalf("expected an applied workspace-create change, got %+v", report.Changes)
	}

	if _, err := os.Stat(filepath.Join(base, "acme", ".tentaqles.yaml")); err != nil {
		t.Fatalf("expected manifest to be scaffolded: %v", err)
	}

	// Re-applying should skip the already-created workspace.
	report2, err := Apply(plan)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	skipped := false
	for _, c := range report2.Changes {
		if c.Kind == "workspace-skip" && c.Target == "acme" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("expected re-apply to skip acme, got %+v", report2.Changes)
	}

	wss, err := ExistingWorkspaces()
	if err != nil {
		t.Fatalf("ExistingWorkspaces: %v", err)
	}
	if len(wss) != 1 || wss[0].Name != "acme" {
		t.Fatalf("expected one workspace named acme, got %+v", wss)
	}
	if !wss[0].Trusted {
		t.Fatalf("expected acme to be trusted, got %+v", wss[0])
	}
}

func TestInstallTQ_CopiesAndIsIdempotent(t *testing.T) {
	home := isolateHome(t)
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "local"))
	}

	var recordedPaths []string
	prevSet := SetUserPath
	SetUserPath = func(dir string) error {
		recordedPaths = append(recordedPaths, dir)
		return nil
	}
	defer func() { SetUserPath = prevSet }()

	src := filepath.Join(home, "tq-src")
	name := "tq"
	if runtime.GOOS == "windows" {
		name = "tq.exe"
	}
	srcPath := filepath.Join(src, name)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcPath, []byte("fake tq binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	dest, err := InstallTQ(srcPath)
	if err != nil {
		t.Fatalf("InstallTQ: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(data) != "fake tq binary" {
		t.Fatalf("unexpected installed content: %q", data)
	}
	if len(recordedPaths) != 1 {
		t.Fatalf("expected SetUserPath to be called once, got %d: %v", len(recordedPaths), recordedPaths)
	}

	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	mtimeBefore := fi.ModTime()

	dest2, err := InstallTQ(srcPath)
	if err != nil {
		t.Fatalf("second InstallTQ: %v", err)
	}
	if dest2 != dest {
		t.Fatalf("expected same dest path, got %q vs %q", dest2, dest)
	}
	fi2, err := os.Stat(dest2)
	if err != nil {
		t.Fatal(err)
	}
	if !fi2.ModTime().Equal(mtimeBefore) {
		t.Fatalf("expected idempotent install to leave file untouched, mtime changed: %v -> %v", mtimeBefore, fi2.ModTime())
	}
	if len(recordedPaths) != 2 {
		t.Fatalf("expected SetUserPath called again on the idempotent path, got %d calls", len(recordedPaths))
	}
}

func TestAddCustomProvider(t *testing.T) {
	isolateHome(t)

	path, err := AddCustomProvider("acme-tool", "Acme Tool", "other", "acme-cli", map[string]string{"ACME_HOME": "{dir}/acme"})
	if err != nil {
		t.Fatalf("AddCustomProvider: %v", err)
	}
	if path == "" {
		t.Fatal("expected a non-empty written path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected provider file to exist: %v", err)
	}

	ps, err := Providers()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range ps {
		if p.ID == "acme-tool" {
			found = true
			if !p.HasCLI || !p.HasIdentity {
				t.Fatalf("expected HasCLI and HasIdentity true, got %+v", p)
			}
		}
	}
	if !found {
		t.Fatal("expected acme-tool to appear in Providers()")
	}

	if _, err := AddCustomProvider("bad id", "x", "other", "", nil); err == nil {
		t.Fatal("expected an error for an invalid provider id")
	}
}
