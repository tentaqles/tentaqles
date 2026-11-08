package paths

import (
	"path/filepath"
	"testing"
)

// TQ_HOME, when set, is the state folder; otherwise it is ~/.tentaqles.
func TestHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TQ_HOME", "")
	if got, want := Home(), filepath.Join(home, ".tentaqles"); got != want {
		t.Errorf("Home() = %q, want %q", got, want)
	}
	alt := filepath.Join(home, "alt")
	t.Setenv("TQ_HOME", alt)
	if got := Home(); got != alt {
		t.Errorf("Home() = %q, want TQ_HOME %q", got, alt)
	}
	if got, want := ManifestPath("acme"), filepath.Join(alt, "identities", "acme", "manifest.yaml"); got != want {
		t.Errorf("ManifestPath = %q, want %q", got, want)
	}
}
