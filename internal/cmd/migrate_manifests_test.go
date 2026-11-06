package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tq migrate manifests: dry run by default, --apply moves the manifest under
// $TQ_HOME and leaves the marker, tq uninstall --restore puts it back.
func TestMigrateManifests_DryRunApplyAndRestore(t *testing.T) {
	e := newMigEnv(t)
	clearAgentMarkers(t)
	root := e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	rootFile := filepath.Join(root, manifest.FileName)
	pp := paths.ManifestPath("alpha")
	before := readFile(t, rootFile)

	code, out, errOut, err := runTQ(t, "migrate", "manifests")
	if err != nil || code != 0 {
		t.Fatalf("dry run: code=%d err=%v stderr=%q", code, err, errOut)
	}
	if !strings.Contains(out, "write-protected-manifest") || !strings.Contains(out, "write-marker") || !strings.HasSuffix(out, "dry run — nothing changed. Re-run with --apply.\n") {
		t.Fatalf("dry run output:\n%s", out)
	}
	if readFile(t, rootFile) != before {
		t.Fatal("a dry run changed the manifest")
	}
	if _, err := os.Lstat(pp); err == nil {
		t.Fatal("a dry run wrote the protected copy")
	}

	code, out, errOut, err = runTQ(t, "migrate", "manifests", "--apply")
	if err != nil || code != 0 {
		t.Fatalf("apply: code=%d err=%v stderr=%q out=%s", code, err, errOut, out)
	}
	// The human-only confirmation names the journal (ADR-0003), then the
	// journal path is printed before anything is touched.
	lines := strings.SplitN(out, "\n", 3)
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "tq migrate --apply: about to write the journal "+filepath.Join(e.tqHome, "backups", migTestTS)) ||
		lines[1] != "journal: "+filepath.Join(e.tqHome, "backups", migTestTS) || !strings.HasSuffix(out, "applied. undo with: tq uninstall --restore "+migTestTS+"\n") {
		t.Fatalf("apply output:\n%s", out)
	}
	if readFile(t, rootFile) != string(manifest.MarkerBytes("alpha")) {
		t.Fatalf("root file must be the marker:\n%s", readFile(t, rootFile))
	}
	if readFile(t, pp) != before {
		t.Fatal("the protected copy must be the manifest's bytes")
	}
	cfg, _ := registry.Load()
	if r := resolve.Resolve(root, cfg); r.Workspace == nil || !r.Workspace.Protected {
		t.Fatalf("alpha must resolve trusted from the protected copy: %+v", r)
	}

	code, out, errOut, err = runTQ(t, "uninstall", "--restore", migTestTS, "--yes")
	if err != nil || code != 0 {
		t.Fatalf("restore: code=%d err=%v stderr=%q out=%s", code, err, errOut, out)
	}
	if readFile(t, rootFile) != before {
		t.Fatalf("restore must put the manifest back:\n%s", readFile(t, rootFile))
	}
	if _, err := os.Lstat(pp); err == nil {
		t.Fatal("restore must remove the protected copy")
	}
	if r := resolve.Resolve(root, cfg); r.Workspace == nil || r.Workspace.Protected {
		t.Fatalf("alpha must be the trusted old layout again: %+v", r)
	}
}

// --apply is human-only, through the subcommand and through --steps; the dry
// run is not.
func TestMigrateManifests_ApplyRefusesAgents(t *testing.T) {
	e := newMigEnv(t)
	clearAgentMarkers(t)
	root := e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	t.Setenv("CLAUDECODE", "1")
	for _, args := range [][]string{{"migrate", "manifests", "--apply"}, {"migrate", "--steps", "manifests", "--apply"}} {
		if _, _, _, err := runTQ(t, args...); !errors.Is(err, errHumanOnly) {
			t.Fatalf("%v: expected the human-only refusal, got %v", args, err)
		}
	}
	if _, err := os.Lstat(paths.ManifestPath("alpha")); err == nil {
		t.Fatal("nothing may be written")
	}
	if code, _, _, err := runTQ(t, "migrate", "manifests"); err != nil || code != 0 {
		t.Fatalf("the dry run is not human-only: %v", err)
	}
	_ = root
}

// The commands that create $TQ_HOME make it private first: init, add and the
// manifests migration.
func TestProtectedHomeIsEnsuredByInitAddAndMigrateManifests(t *testing.T) {
	e := newMigEnv(t)
	clearAgentMarkers(t)
	calls := stubProtectedHome(t)
	prevGit := addRunGit
	addRunGit = func(...string) (string, error) { return "", nil }
	t.Cleanup(func() { addRunGit = prevGit })

	if _, _, _, err := runTQ(t, "init", e.base); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatalf("tq init: calls = %d", *calls)
	}
	if _, _, _, err := runTQ(t, "add", "acme", "--git-email", "a@acme.test"); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatalf("tq add: calls = %d", *calls)
	}
	if _, _, _, err := runTQ(t, "migrate", "manifests"); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatalf("a dry run must not touch the home: calls = %d", *calls)
	}
	if _, _, _, err := runTQ(t, "migrate", "manifests", "--apply"); err != nil {
		t.Fatal(err)
	}
	if *calls != 3 {
		t.Fatalf("tq migrate manifests --apply: calls = %d", *calls)
	}
}
