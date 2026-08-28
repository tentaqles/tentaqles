package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/hooks"
)

func TestHooksStatus_Command(t *testing.T) {
	dir := t.TempDir()
	prev := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles {
		return hooks.Profiles{"bash": filepath.Join(dir, "bashrc")}
	}
	t.Cleanup(func() { hooks.ProfilesFn = prev })

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"hooks", "status"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "bash") {
		t.Fatalf("expected bash in output, got: %s", out.String())
	}
}

func TestHooksStatus_NoProfileForAbsentFile(t *testing.T) {
	dir := t.TempDir()
	prev := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles {
		return hooks.Profiles{"zsh": filepath.Join(dir, "nonexistent-zshrc")}
	}
	t.Cleanup(func() { hooks.ProfilesFn = prev })

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"hooks", "status"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "zsh") {
			found = true
			if !strings.Contains(l, "no profile") {
				t.Fatalf("expected zsh line to report 'no profile' for a nonexistent path, got: %q", l)
			}
			if strings.Contains(l, "missing") {
				t.Fatalf("did not expect 'missing' for a nonexistent path, got: %q", l)
			}
		}
	}
	if !found {
		t.Fatalf("expected a zsh line in output: %s", out.String())
	}
}

func TestHooksInstallAndRemove_Command(t *testing.T) {
	dir := t.TempDir()
	prev := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles {
		return hooks.Profiles{"bash": filepath.Join(dir, "bashrc")}
	}
	t.Cleanup(func() { hooks.ProfilesFn = prev })

	root := NewRoot()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"hooks", "install", "bash"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# >>> tq >>>") {
		t.Fatal("expected block installed")
	}

	root2 := NewRoot()
	root2.SetOut(&bytes.Buffer{})
	root2.SetArgs([]string{"hooks", "remove", "bash"})
	if err := root2.Execute(); err != nil {
		t.Fatal(err)
	}
	data2, err := os.ReadFile(filepath.Join(dir, "bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data2), "# >>> tq >>>") {
		t.Fatal("expected block removed")
	}
}
