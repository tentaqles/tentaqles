package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// A copy of a recorded client's marker in a second folder gets no identity: env names no workspace and
// prints none of the client's identity folder.
func TestEnv_SecondFolderGetsNoIdentity(t *testing.T) {
	dir := existingClient(t)
	copyDir := copiedClient(t, filepath.Dir(dir), "acme")

	// Control: the real folder carries acme.
	chdir(t, dir)
	if got := runEnv(t, "env", "--shell", "bash", "--json"); !strings.Contains(got, `"workspace":"acme"`) {
		t.Fatalf("control: the real folder does not carry acme: %s", got)
	}

	chdir(t, copyDir)
	got := runEnv(t, "env", "--shell", "bash", "--json")
	if strings.Contains(got, `"workspace":"acme"`) {
		t.Fatalf("the copy carries acme: %s", got)
	}
	idDir := paths.IdentityDir("acme", "claude")
	for _, p := range []string{idDir, filepath.ToSlash(idDir), strings.ReplaceAll(idDir, `\`, `\\`)} {
		if strings.Contains(got, p) {
			t.Fatalf("the copy's env names acme's identity folder %q: %s", p, got)
		}
	}
}

// tentaqles run . resolves the client of the working folder: none for a copy, acme for the real folder.
// The function run . calls is tested, not a child process.
func TestRunDot_SecondFolderHasNoIdentity(t *testing.T) {
	dir := existingClient(t)
	copyDir := copiedClient(t, filepath.Dir(dir), "acme")

	chdir(t, copyDir)
	if w, err := cwdWorkspace(); err != nil || w != nil {
		t.Fatalf("the copy resolves for run .: workspace %v, err %v", w, err)
	}
	chdir(t, dir)
	if w, err := cwdWorkspace(); err != nil || w == nil || w.Name != "acme" {
		t.Fatalf("the real folder does not resolve to acme for run .: workspace %v, err %v", w, err)
	}
}

// A shell call from a copy's folder is denied by the pre-tool-use hook, and the same call from the real
// folder is allowed.
func TestPreToolUse_CopiedFolderDenies(t *testing.T) {
	dir := existingClient(t)
	copyDir := copiedClient(t, filepath.Dir(dir), "acme")

	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, copyDir, "ls"))
	if code != 2 {
		t.Fatalf("a shell call from a copy's folder: code=%d, want deny (2)\n%s", code, errOut)
	}
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, "ls")); code != 0 {
		t.Fatalf("control: the same call from the real folder: code=%d\n%s", code, errOut)
	}
}
