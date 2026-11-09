package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// A client with a memory: drop deletes the memory with the identity folder, and says so.
func TestDrop_RemovesTheLobe(t *testing.T) {
	existingClient(t)
	if _, err := store.Open("acme", store.ReadWrite); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(paths.LobeDir("acme"), "store.json")) {
		t.Fatal("the client has no lobe store before the drop")
	}
	chdir(t, t.TempDir())
	out, err := runDrop(t, "acme\n", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if exists(filepath.Join(paths.IdentitiesRoot(), "acme")) {
		t.Fatal("the identity folder, with the memory in it, survived the drop")
	}
	if !strings.Contains(out, "and the client's memory") {
		t.Errorf("the confirmation does not name the memory:\n%s", out)
	}
}
