package bundle

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSkills_CopyPruneAndRefuseUnmanaged(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "identity")

	srcA := filepath.Join(root, "src", "alpha")
	writeFile(t, filepath.Join(srcA, "SKILL.md"), "alpha content")
	writeFile(t, filepath.Join(srcA, "sub", "file.txt"), "nested")

	d := Desired{Skills: map[string]string{"alpha": srcA}}
	st := &State{Skills: []string{}}

	changed, err := SyncSkills(dir, d, st)
	if err != nil {
		t.Fatalf("SyncSkills: %v", err)
	}
	if len(changed) != 1 || changed[0] != "alpha" {
		t.Fatalf("changed = %v, want [alpha]", changed)
	}

	dst := filepath.Join(dir, "skills", "alpha")
	got, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil || string(got) != "alpha content" {
		t.Fatalf("SKILL.md not copied correctly: %v %q", err, got)
	}
	gotNested, err := os.ReadFile(filepath.Join(dst, "sub", "file.txt"))
	if err != nil || string(gotNested) != "nested" {
		t.Fatalf("nested file not copied correctly: %v %q", err, gotNested)
	}
	marker, err := os.ReadFile(filepath.Join(dst, ".tq-managed"))
	if err != nil {
		t.Fatalf("marker not written: %v", err)
	}
	if string(marker) != srcA {
		t.Fatalf("marker content = %q, want %q", marker, srcA)
	}
	if !sort.StringsAreSorted(st.Skills) {
		t.Fatalf("st.Skills not sorted: %v", st.Skills)
	}
	if len(st.Skills) != 1 || st.Skills[0] != "alpha" {
		t.Fatalf("st.Skills = %v, want [alpha]", st.Skills)
	}

	// Now desired changes to no skills; alpha (managed) should be pruned.
	d2 := Desired{Skills: map[string]string{}}
	st2 := &State{Skills: []string{"alpha"}}
	changed2, err := SyncSkills(dir, d2, st2)
	if err != nil {
		t.Fatalf("SyncSkills prune: %v", err)
	}
	if len(changed2) != 0 {
		t.Fatalf("changed2 = %v, want empty (prunes aren't 'changed' additions? verify)", changed2)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("expected alpha dir pruned, stat err = %v", err)
	}
	if len(st2.Skills) != 0 {
		t.Fatalf("st2.Skills = %v, want empty", st2.Skills)
	}

	// Unmanaged skill dir (no marker) should block sync.
	unmanagedDir := filepath.Join(dir, "skills", "beta")
	writeFile(t, filepath.Join(unmanagedDir, "note.txt"), "hand-authored")
	d3 := Desired{Skills: map[string]string{"beta": srcA}}
	st3 := &State{}
	_, err = SyncSkills(dir, d3, st3)
	if err == nil {
		t.Fatalf("expected error for unmanaged skill, got nil")
	}
}
