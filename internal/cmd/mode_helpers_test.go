package cmd

import (
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// Manifest bodies the mode tests (tq allow --mode, tq run) set up with.
const (
	relaxedWorkspace = "guard:\n  mode: relaxed\n"
	legacyProject    = "guard:\n  unattended:\n    projects: [demo-todo]\n"
)

// wsFor resolves the trusted workspace acme at root for the mode tests.
func wsFor(t *testing.T, root string) *resolve.Workspace {
	t.Helper()
	mp := filepath.Join(root, manifest.FileName)
	m, err := manifest.Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(mp)
	return &resolve.Workspace{Name: "acme", Root: root, ManifestPath: mp, Hash: h, Manifest: m}
}
