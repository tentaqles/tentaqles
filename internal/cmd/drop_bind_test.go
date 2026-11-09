package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
)

// Dropping a client removes its record.
func TestDrop_RemovesTheBinding(t *testing.T) {
	existingClient(t)
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Folder("acme"); !ok {
		t.Fatal("acme is not recorded after reach")
	}
	chdir(t, t.TempDir())
	if out, err := runDrop(t, "acme\n", "acme"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, err = registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Folder("acme"); ok {
		t.Fatal("acme is still recorded after drop")
	}
}

// A copy of a client's marker in another folder must never drop the real client.
func TestDrop_RefusesACopiedFolder(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	one := filepath.Join(clients, "acme")
	if out, err := runReach(t, one); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	copyDir := copiedClient(t, clients, "acme")
	chdir(t, t.TempDir())
	_, planErr := planDrop(copyDir)
	if planErr == nil {
		t.Fatal("a copied folder was planned for drop")
	}
	want, err := registry.Normalize(one)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(planErr.Error()), strings.ToLower(want)) {
		t.Fatalf("the refusal does not name the client's folder %s: %v", want, planErr)
	}
	if !exists(paths.ManifestPath("acme")) || !exists(filepath.Join(one, manifest.FileName)) {
		t.Fatal("a refused drop deleted something")
	}
}
