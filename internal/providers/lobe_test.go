package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A user provider file named lobe.yaml is refused: lobe is the folder of a client's memory, not a
// provider. LoadAll reports the file the way it reports any other bad file, and the catalog has no lobe.
func TestProviderIDLobeIsReserved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", home)
	dir := filepath.Join(home, "providers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "id: lobe\nname: Lobe\ncategory: other\ncli: { command: lobe }\nmatch:\n  executables: [lobe]\n"
	if err := os.WriteFile(filepath.Join(dir, "lobe.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, errs := LoadAll()
	if c == nil {
		t.Fatal("LoadAll returned no catalog")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "reserved") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no reserved error in %v", errs)
	}
	if _, ok := c.Get("lobe"); ok {
		t.Fatal("the provider lobe is in the catalog")
	}
}
