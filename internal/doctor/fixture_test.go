package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// markerFor writes the client marker in root (the folder's name is the client's) and returns the
// path the test writes the client's manifest to: paths.ManifestPath of that name.
func markerFor(t *testing.T, root string) string {
	t.Helper()
	name := filepath.Base(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifest.FileName), manifest.MarkerBytes(name), 0o644); err != nil {
		t.Fatal(err)
	}
	mp := paths.ManifestPath(name)
	if err := os.MkdirAll(filepath.Dir(mp), 0o700); err != nil {
		t.Fatal(err)
	}
	return mp
}
