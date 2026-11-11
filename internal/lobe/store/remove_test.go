package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestLobeStoreRemove: Remove deletes a regular file of the store, does nothing for a file that is not
// there, and refuses a path that leaves the store, a folder that is not a folder, and a link.
func TestLobeStoreRemove(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	dir := filepath.Join(st.Dir(), "events")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "events-20200101.jsonl")
	if err := os.WriteFile(gone, []byte("x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := st.Remove("events/events-20200101.jsonl"); err != nil {
		t.Fatalf("Remove regular file: %v", err)
	}
	if _, err := os.Lstat(gone); !os.IsNotExist(err) {
		t.Errorf("file still there after Remove: %v", err)
	}
	if err := st.Remove("events/events-20200101.jsonl"); err != nil {
		t.Errorf("Remove of a missing file = %v, want nil", err)
	}
	for _, rel := range []string{"../events/x.jsonl", "/events/x.jsonl", "events/../../x", "events\\x.jsonl"} {
		if err := st.Remove(rel); !errors.Is(err, ErrPath) {
			t.Errorf("Remove(%q) = %v, want ErrPath", rel, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := st.Remove("events/sub.jsonl"); !errors.Is(err, ErrLinked) {
		t.Errorf("Remove of a folder = %v, want ErrLinked", err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir(), "notes"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	// The parent of this path is a file, not a folder: refused.
	if err := st.Remove("notes/x.jsonl"); !errors.Is(err, ErrLinked) {
		t.Errorf("Remove under a file = %v, want ErrLinked", err)
	}
}
