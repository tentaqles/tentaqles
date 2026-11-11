package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestLobeStoreRemoveRefusesLink: Remove refuses a link in the store and leaves its target alone. It skips
// where the platform cannot make a symbolic link without a privilege.
func TestLobeStoreRemoveRefusesLink(t *testing.T) {
	setupHome(t)
	st := openAcme(t)
	dir := filepath.Join(st.Dir(), "events")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(target, []byte("x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "events-20200102.jsonl")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := st.Remove("events/events-20200102.jsonl"); !errors.Is(err, ErrLinked) {
		t.Errorf("Remove of a link = %v, want ErrLinked", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("link target was touched: %v", err)
	}
}
