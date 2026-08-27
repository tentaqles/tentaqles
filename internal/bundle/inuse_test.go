package bundle

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInUse(t *testing.T) {
	dir := t.TempDir()

	if InUse(dir) {
		t.Fatalf("InUse should be false when sessions dir is missing")
	}

	sessions := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if InUse(dir) {
		t.Fatalf("InUse should be false when sessions dir is empty")
	}

	fresh := filepath.Join(sessions, "fresh.json")
	if err := os.WriteFile(fresh, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !InUse(dir) {
		t.Fatalf("InUse should be true for a freshly modified session file")
	}

	stale := filepath.Join(sessions, "stale.json")
	if err := os.WriteFile(stale, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-20 * time.Minute)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fresh); err != nil {
		t.Fatal(err)
	}
	if InUse(dir) {
		t.Fatalf("InUse should be false when all session files are stale")
	}
}
