package setupapi

import (
	"github.com/tentaqles/tentaqles/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// InstallTQ updates the persistent user PATH, which an already-running process
// never sees. A PATH-only lookup therefore reports "not installed" right after
// a successful install -- the first thing a new user does.
func TestTQPath_FallsBackToTheInstallDirectory(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", dir)
	} else {
		t.Setenv("HOME", dir)
	}
	// Nothing on PATH, nothing installed yet.
	t.Setenv("PATH", filepath.Join(dir, "definitely-empty"))
	if got := TQPath(); got != "" {
		t.Fatalf("with no tq anywhere, want empty, got %q", got)
	}

	destDir, err := installDestDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "tq"
	if runtime.GOOS == "windows" {
		name = "tq.exe"
	}
	want := filepath.Join(destDir, name)
	if err := os.WriteFile(want, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := TQPath(); got != want {
		t.Fatalf("TQPath() = %q, want the install-dir copy %q", got, want)
	}
}

// Someone adopting tq usually has the folders already. Making them retype the
// names into an empty form is tedious and invites a typo in a name that has to
// match the directory exactly.
func TestBaseFolders_OffersWhatIsAlreadyThere(t *testing.T) {
	base := t.TempDir()
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{base}, parts...)...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// A client folder with two repos, the first carrying an identity.
	mk("acme", "api", ".git")
	mk("acme", "web", ".git")
	if err := os.WriteFile(filepath.Join(base, "acme", "api", ".git", "config"),
		[]byte("[core]\n\tbare = false\n[user]\n\tname = Acme Dev\n\temail = dev@acme.test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A folder that is already a tq workspace.
	mk("managed")
	if err := os.WriteFile(filepath.Join(base, "managed", manifestFileNameForTest()), []byte("client: managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A plain folder with no repos, and a dotfolder that should be ignored.
	mk("empty")
	mk(".hidden")

	got, err := BaseFolders(base)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]FolderCandidate{}
	for _, c := range got {
		byName[c.Name] = c
	}
	if _, ok := byName[".hidden"]; ok {
		t.Error("dotfolders are not companies")
	}
	if len(got) != 3 {
		t.Fatalf("want acme, empty, managed; got %d: %+v", len(got), got)
	}
	if got[0].Name != "acme" || got[1].Name != "empty" || got[2].Name != "managed" {
		t.Errorf("want them sorted, got %v %v %v", got[0].Name, got[1].Name, got[2].Name)
	}

	acme := byName["acme"]
	if acme.Repos != 2 {
		t.Errorf("acme repos = %d, want 2", acme.Repos)
	}
	if acme.GitName != "Acme Dev" || acme.GitEmail != "dev@acme.test" {
		t.Errorf("acme identity = %q/%q, want the one its repos already use", acme.GitName, acme.GitEmail)
	}
	if acme.Managed {
		t.Error("acme is not a tq workspace yet")
	}
	if !byName["managed"].Managed {
		t.Error("a folder with a manifest is already managed and must be marked so")
	}
	if byName["empty"].Repos != 0 || byName["empty"].GitEmail != "" {
		t.Errorf("empty folder should suggest nothing: %+v", byName["empty"])
	}
}

func TestBaseFolders_MissingBaseIsNotAnError(t *testing.T) {
	got, err := BaseFolders(filepath.Join(t.TempDir(), "not-created-yet"))
	if err != nil {
		t.Fatalf("a work folder that does not exist yet is normal: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want none, got %+v", got)
	}
	if got, err := BaseFolders("  "); err != nil || len(got) != 0 {
		t.Errorf("empty base: %+v %v", got, err)
	}
}

func manifestFileNameForTest() string { return ".tentaqles.yaml" }

// A .git/config written by a Windows tool can carry a UTF-8 BOM, which would
// make the first section header unrecognisable and silently suggest nothing.
func TestBaseFolders_ToleratesABOMInGitConfig(t *testing.T) {
	base := t.TempDir()
	g := filepath.Join(base, "acme", "api", ".git")
	if err := os.MkdirAll(g, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[user]\n\tname = Acme Dev\n\temail = dev@acme.test\n"
	if err := os.WriteFile(filepath.Join(g, "config"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := BaseFolders(base)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got[0].GitEmail != "dev@acme.test" {
		t.Errorf("email = %q, want it read despite the BOM", got[0].GitEmail)
	}
}

// A stale bundled tq replaced a working install on the development machine and
// silently removed a subcommand the Claude Code plugin depends on, so every
// session in every workspace began erroring on each command. An install button
// must not be able to do that quietly.
func TestInstallTQ_RefusesToReplaceADifferentVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs an executable script; the shell shim differs on Windows")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	destDir, err := installDestDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := func(path, version string) {
		body := "#!/bin/sh\necho '" + version + "'\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installed := filepath.Join(destDir, "tq")
	fake(installed, "tq 0.9.9")
	incoming := filepath.Join(dir, "tq")
	fake(incoming, "tq 0.1.0")

	_, err = InstallTQ(incoming)
	if err == nil {
		t.Fatal("installing a different version over an existing one must be refused")
	}
	for _, want := range []string{"0.9.9", "0.1.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should name both versions so the user can judge; got: %v", err)
		}
	}
	// and the installed binary must be untouched
	got, _ := os.ReadFile(installed)
	if !strings.Contains(string(got), "0.9.9") {
		t.Error("the existing binary must not have been replaced")
	}
}
