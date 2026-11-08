package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
)

func runDrop(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"drop"}, args...))
	err := root.Execute()
	return out.String(), err
}

// clientParts are the paths reach leaves for a client; drop removes every one.
func clientParts(name, dir string) []string {
	return []string{filepath.Join(paths.IdentitiesRoot(), name), paths.ShimsDir(name),
		filepath.Join(dir, manifest.FileName), gitcfg.WorkspaceFile(dir)}
}

func bases(t *testing.T) []string {
	t.Helper()
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Bases
}

// Dropping one of two clients removes all of it, keeps the other and its git rule, keeps the clients
// folder registered, and leaves the user's files in the dropped folder.
func TestDrop_UndoesTheClient(t *testing.T) {
	dir := existingClient(t)
	clients := filepath.Dir(dir)
	other := filepath.Join(clients, "other")
	if out, err := runReach(t, other); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	writeTB(t, filepath.Join(dir, "notes.txt"), "mine")
	chdir(t, t.TempDir())
	out, err := runDrop(t, "acme\n", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, p := range clientParts("acme", dir) {
		if exists(p) {
			t.Errorf("%s survived the drop", p)
		}
	}
	for _, p := range clientParts("other", other) {
		if p != paths.ShimsDir("other") && !exists(p) {
			t.Errorf("%s of another client was deleted", p)
		}
	}
	inc := string(mustRead(t, gitcfg.IncludeFile()))
	if strings.Contains(inc, "/acme/") || !strings.Contains(inc, "/other/") {
		t.Errorf("the git rules after the drop:\n%s", inc)
	}
	if !exists(filepath.Join(dir, "notes.txt")) {
		t.Fatal("the user's file was deleted")
	}
	if !slices.ContainsFunc(bases(t), func(b string) bool { return registry.SamePath(b, clients) }) {
		t.Error("the clients folder was unregistered while another client is in it")
	}
	if _, err := findWorkspace("acme"); err == nil {
		t.Error("acme still resolves")
	}
	for _, want := range []string{"about to remove the client acme", "Type the client's name (acme)", "acme is dropped", "stays, with your files"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// The last client of a clients folder, named by its folder: the registration and the emptied folder go
// too, as if reach never ran.
func TestDrop_LastClientLeavesNoTrace(t *testing.T) {
	dir := existingClient(t)
	clients := filepath.Dir(dir)
	chdir(t, t.TempDir())
	if out, err := runDrop(t, "", dir, "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if exists(dir) {
		entries, _ := os.ReadDir(dir)
		t.Fatalf("the emptied folder stays: %v", entries)
	}
	if slices.ContainsFunc(bases(t), func(b string) bool { return registry.SamePath(b, clients) }) {
		t.Error("the clients folder is still registered")
	}
	if inc := string(mustRead(t, gitcfg.IncludeFile())); strings.Contains(inc, "includeIf") {
		t.Errorf("a git rule is left:\n%s", inc)
	}
}

// Without the client's name typed back nothing is deleted: a plain yes is not enough.
func TestDrop_DeclinedDeletesNothing(t *testing.T) {
	dir := existingClient(t)
	before := mustRead(t, gitcfg.IncludeFile())
	if out, err := runDrop(t, "yes\n", "acme"); !errors.Is(err, errAborted) {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, p := range clientParts("acme", dir) {
		if p != paths.ShimsDir("acme") && !exists(p) {
			t.Errorf("%s was deleted without the name typed", p)
		}
	}
	if !bytes.Equal(before, mustRead(t, gitcfg.IncludeFile())) {
		t.Error("the git rules changed")
	}
}

// Refused: inside Claude Code, a path for a name, an unknown client, a folder without a marker.
func TestDrop_Refusals(t *testing.T) {
	dir := existingClient(t)
	t.Setenv("CLAUDECODE", "1")
	if _, err := runDrop(t, "acme\n", "acme"); err == nil || !strings.Contains(err.Error(), "your own terminal") {
		t.Fatalf("inside Claude Code: %v", err)
	}
	unsetenv(t, "CLAUDECODE")
	if _, err := runDrop(t, "", "../acme", "--yes"); err == nil {
		t.Fatal("a path was taken for a name")
	}
	if _, err := runDrop(t, "", "nobody", "--yes"); err == nil || !strings.Contains(err.Error(), `no client "nobody"`) {
		t.Fatalf("an unknown client: %v", err)
	}
	if _, err := runDrop(t, "", t.TempDir(), "--yes"); err == nil || !strings.Contains(err.Error(), "no tentaqles client marker") {
		t.Fatalf("a folder without a marker: %v", err)
	}
	if !exists(filepath.Join(dir, manifest.FileName)) || !exists(paths.ManifestPath("acme")) {
		t.Fatal("a refused drop deleted something")
	}
}

// A client half removed by hand (its folder gone, its identity folder left) still drops.
func TestDrop_LeftoverIdentityFolder(t *testing.T) {
	isolateHome(t)
	writeTB(t, filepath.Join(paths.IdentityDir("ghost", "claude"), "settings.json"), "{}")
	if out, err := runDrop(t, "ghost\n", "ghost"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if exists(filepath.Join(paths.IdentitiesRoot(), "ghost")) {
		t.Fatal("the identity folder stays")
	}
}
