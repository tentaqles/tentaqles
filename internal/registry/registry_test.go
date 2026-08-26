package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Missing_IsEmpty(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	c, err := Load()
	if err != nil || len(c.Bases) != 0 {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestAddBase_SaveLoad_Roundtrip_Dedup(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := t.TempDir()
	c, _ := Load()
	if added, err := c.AddBase(base); err != nil || !added {
		t.Fatalf("first add: %v %v", added, err)
	}
	if added, _ := c.AddBase(base + string(os.PathSeparator)); added {
		t.Fatal("trailing separator should dedup")
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := Load()
	if err != nil || len(c2.Bases) != 1 || c2.Bases[0] != filepath.Clean(base) {
		t.Fatalf("roundtrip: %+v %v", c2, err)
	}
}

func TestAddBase_NonexistentDir_Error(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	c, _ := Load()
	if _, err := c.AddBase(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error")
	}
}
