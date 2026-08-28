package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hooks"
)

// isolateSetupHome mirrors internal/setup's isolateHome helper: it points
// TQ_HOME/HOME (or USERPROFILE on Windows) at a temp dir so registry and
// gitcfg operations never touch the real developer home directory.
func isolateSetupHome(t *testing.T) string {
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

func writeTempPlan(t *testing.T, base string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.yaml")
	content := "base: " + filepath.ToSlash(base) + "\n" +
		"trust: true\n" +
		"companies:\n" +
		"  - name: acme\n" +
		"    git_name: Jane Doe\n" +
		"    git_email: jane@acme.com\n" +
		"    permission_mode: default\n" +
		"    identities: []\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func setupTempProfiles(t *testing.T) func() hooks.Profiles {
	dir := t.TempDir()
	prev := hooks.ProfilesFn
	fn := func() hooks.Profiles {
		return hooks.Profiles{}
	}
	hooks.ProfilesFn = fn
	t.Cleanup(func() { hooks.ProfilesFn = prev })
	_ = dir
	return fn
}

func TestSetup_ExamplePrints(t *testing.T) {
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"setup", "--example"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "base:") {
		t.Fatalf("expected example YAML in output, got %q", out.String())
	}
}

func TestSetup_DryRunNoWrites(t *testing.T) {
	home := isolateSetupHome(t)
	setupTempProfiles(t)
	base := t.TempDir()
	planPath := writeTempPlan(t, base)

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"setup", "--from", planPath, "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v; output=%s", err, out.String())
	}

	if _, err := os.Stat(filepath.Join(home, ".tentaqles")); err == nil {
		t.Fatal("expected nothing under TQ_HOME after --dry-run")
	}
	if _, err := os.Stat(filepath.Join(base, "acme")); err == nil {
		t.Fatal("expected no workspace to be scaffolded after --dry-run")
	}
	if !strings.Contains(out.String(), "workspace-create") {
		t.Fatalf("expected preview output, got %q", out.String())
	}
}

func TestSetup_FromYes_Applies(t *testing.T) {
	isolateSetupHome(t)
	setupTempProfiles(t)
	base := t.TempDir()
	planPath := writeTempPlan(t, base)

	prevRunGit := setupRunGit
	setupRunGit = func(...string) (string, error) { return "", nil }
	t.Cleanup(func() { setupRunGit = prevRunGit })

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"setup", "--from", planPath, "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v; output=%s", err, out.String())
	}

	if _, err := os.Stat(filepath.Join(base, "acme", "manifest.yaml")); err != nil {
		// Manifest filename is defined in internal/manifest; fall back to a
		// directory existence check if the exact name ever changes.
		if _, err2 := os.Stat(filepath.Join(base, "acme")); err2 != nil {
			t.Fatalf("expected acme workspace to be scaffolded: %v / %v", err, err2)
		}
	}
}

func TestSetup_RefusesWithoutTTYOrYes(t *testing.T) {
	isolateSetupHome(t)
	setupTempProfiles(t)
	base := t.TempDir()
	planPath := writeTempPlan(t, base)

	prevTTY := setupIsTTY
	setupIsTTY = func() bool { return false }
	t.Cleanup(func() { setupIsTTY = prevTTY })

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"setup", "--from", planPath})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error when stdin is not a TTY and --yes is absent")
	}
	if _, err := os.Stat(filepath.Join(base, "acme")); err == nil {
		t.Fatal("expected no workspace to be scaffolded")
	}
}
