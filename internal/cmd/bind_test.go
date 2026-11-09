package cmd

import (
	"bytes"
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

// runBind runs tentaqles bind with stdin on its input and returns its output.
func runBind(t *testing.T, stdin string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs([]string{"bind"})
	err := root.Execute()
	return out.String(), err
}

// registryBytes is the registry file as it is on disk.
func registryBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(paths.Config())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// unbindClient removes the record of client, as a client created before folders were recorded has none.
func unbindClient(t *testing.T, client string) {
	t.Helper()
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Unbind(client)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
}

// copiedClient registers a second clients folder beside clients, puts a copy of client's marker in it,
// and returns the copy's folder.
func copiedClient(t *testing.T, clients, client string) string {
	t.Helper()
	two := filepath.Join(filepath.Dir(clients), "two")
	dir := filepath.Join(two, client)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(clients, client, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), marker, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddBase(two); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBind_RefusedForAgents(t *testing.T) {
	dir := existingClient(t)
	unbindClient(t, "acme")
	before := registryBytes(t)
	chdir(t, dir)
	t.Setenv("CLAUDECODE", "1")
	out, err := runBind(t, "yes\n")
	if err == nil || !strings.Contains(err.Error(), "your own terminal") {
		t.Fatalf("inside Claude Code: err = %v\n%s", err, out)
	}
	if !bytes.Equal(before, registryBytes(t)) {
		t.Fatal("a refused bind changed the registry")
	}
}

func TestBind_DeclinedWritesNothing(t *testing.T) {
	dir := existingClient(t)
	unbindClient(t, "acme")
	before := registryBytes(t)
	chdir(t, dir)
	out, err := runBind(t, "no\n")
	if !errors.Is(err, errAborted) {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !bytes.Equal(before, registryBytes(t)) {
		t.Fatal("a declined bind changed the registry")
	}
}

func TestBind_BindsAClientWithNoRecord(t *testing.T) {
	dir := existingClient(t)
	unbindClient(t, "acme")
	sub := filepath.Join(dir, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)
	if out, err := runBind(t, "yes\n"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want, err := registry.Normalize(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := cfg.Folder("acme")
	if !ok || !registry.SamePath(rec, want) {
		t.Fatalf("acme is recorded as %q (%v), want %s", rec, ok, want)
	}
}

func TestBind_MovesTheRecord(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	one := filepath.Join(clients, "acme")
	if out, err := runReach(t, one); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	two := copiedClient(t, clients, "acme")
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if r := resolve.Resolve(two, cfg); r.Workspace != nil {
		t.Fatal("the copy resolves before bind")
	}
	chdir(t, two)
	if out, err := runBind(t, "yes\n"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, err = registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if r := resolve.Resolve(two, cfg); r.Workspace == nil {
		t.Fatalf("the moved client does not resolve after bind: %s", r.Reason)
	}
	if r := resolve.Resolve(one, cfg); r.Workspace != nil || !r.Unreadable {
		t.Fatalf("the old folder still resolves after bind: workspace %v, reason %q", r.Workspace, r.Reason)
	}
}

func TestBind_NotAClientsFolder(t *testing.T) {
	existingClient(t)
	before := registryBytes(t)
	chdir(t, t.TempDir())
	out, err := runBind(t, "yes\n")
	if err == nil {
		t.Fatalf("a folder outside every base was bound\n%s", out)
	}
	if !bytes.Equal(before, registryBytes(t)) {
		t.Fatal("a refused bind changed the registry")
	}
}

func TestBind_AlreadyBoundIsANoOp(t *testing.T) {
	dir := existingClient(t)
	before := registryBytes(t)
	chdir(t, dir)
	out, err := runBind(t, "")
	if err != nil || !strings.Contains(out, "nothing to do") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !bytes.Equal(before, registryBytes(t)) {
		t.Fatal("a bind with nothing to do changed the registry")
	}
}
