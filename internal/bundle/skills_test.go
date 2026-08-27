package bundle

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
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
	source, sha, ok := parseSkillMarker(marker)
	if !ok || source != srcA || sha == "" {
		t.Fatalf("marker malformed: %q (source=%q sha=%q ok=%v)", marker, source, sha, ok)
	}
	if !sort.StringsAreSorted(st.Skills) {
		t.Fatalf("st.Skills not sorted: %v", st.Skills)
	}
	if len(st.Skills) != 1 || st.Skills[0] != "alpha" {
		t.Fatalf("st.Skills = %v, want [alpha]", st.Skills)
	}

	// Now desired changes to no skills; alpha (managed) should be pruned
	// and reported as removed.
	d2 := Desired{Skills: map[string]string{}}
	st2 := &State{Skills: []string{"alpha"}}
	changed2, err := SyncSkills(dir, d2, st2)
	if err != nil {
		t.Fatalf("SyncSkills prune: %v", err)
	}
	if len(changed2) != 1 || changed2[0] != "-alpha" {
		t.Fatalf("changed2 = %v, want [-alpha]", changed2)
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

func TestSyncSkills_SecondRunNoChange(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "identity")
	src := filepath.Join(root, "src", "alpha")
	writeFile(t, filepath.Join(src, "SKILL.md"), "alpha content")

	d := Desired{Skills: map[string]string{"alpha": src}}
	st := &State{}

	if _, err := SyncSkills(dir, d, st); err != nil {
		t.Fatalf("first SyncSkills: %v", err)
	}

	dst := filepath.Join(dir, "skills", "alpha", "SKILL.md")
	before, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure any timestamp-based accidental rewrite would be observable.
	time.Sleep(10 * time.Millisecond)

	st2 := &State{Skills: st.Skills}
	changed, err := SyncSkills(dir, d, st2)
	if err != nil {
		t.Fatalf("second SyncSkills: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want empty on unchanged second run", changed)
	}

	after, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("destination file was rewritten: before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

func TestSyncSkills_SourceChangedIsRecopied(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "identity")
	src := filepath.Join(root, "src", "alpha")
	writeFile(t, filepath.Join(src, "SKILL.md"), "v1")

	d := Desired{Skills: map[string]string{"alpha": src}}
	st := &State{}
	if _, err := SyncSkills(dir, d, st); err != nil {
		t.Fatalf("first SyncSkills: %v", err)
	}

	writeFile(t, filepath.Join(src, "SKILL.md"), "v2")

	st2 := &State{Skills: st.Skills}
	changed, err := SyncSkills(dir, d, st2)
	if err != nil {
		t.Fatalf("second SyncSkills: %v", err)
	}
	if len(changed) != 1 || changed[0] != "alpha" {
		t.Fatalf("changed = %v, want [alpha] when source content changed", changed)
	}

	got, err := os.ReadFile(filepath.Join(dir, "skills", "alpha", "SKILL.md"))
	if err != nil || string(got) != "v2" {
		t.Fatalf("expected recopy to pick up v2, got %q (err=%v)", got, err)
	}
}

func TestSyncSkills_RejectsTraversalName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "identity")

	// Sentinel sitting next to the skills dir: if a traversal name like ".."
	// resolved to skillsRoot's parent, RemoveAll would wipe this out.
	sentinel := filepath.Join(dir, "sentinel.txt")
	writeFile(t, sentinel, "must survive")

	src := filepath.Join(root, "src", "alpha")
	writeFile(t, filepath.Join(src, "SKILL.md"), "content")

	d := Desired{Skills: map[string]string{"..": src}}
	st := &State{}
	_, err := SyncSkills(dir, d, st)
	if err == nil {
		t.Fatalf("expected error for traversal skill name")
	}

	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Fatalf("sentinel file should still exist: %v", statErr)
	}
}

func TestCopyDir_FollowsSymlinkedDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation on Windows typically requires elevated privileges; skip if unavailable")
	}

	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	writeFile(t, filepath.Join(realDir, "file.txt"), "via symlink")

	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(src, "linked")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("cannot create symlink in this environment: %v", err)
	}

	dst := filepath.Join(root, "dst")
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	info, err := os.Lstat(filepath.Join(dst, "linked"))
	if err != nil {
		t.Fatalf("expected linked dir copied: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected a real directory at dst, got a symlink")
	}
	got, err := os.ReadFile(filepath.Join(dst, "linked", "file.txt"))
	if err != nil || string(got) != "via symlink" {
		t.Fatalf("expected symlinked dir contents copied: %v %q", err, got)
	}
}

// linkDir creates dir as a link to target, returning false if the platform
// or privileges don't allow it.
func linkDir(t *testing.T, target, link string) bool {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	if err := cmd.Run(); err != nil {
		t.Logf("mklink /J failed: %v", err)
		return false
	}
	return true
}

func TestSyncSkills_RefusesSymlinkedRoot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "identity")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	shared := filepath.Join(root, "shared-skills")
	writeFile(t, filepath.Join(shared, "handwritten", "SKILL.md"), "precious")

	skillsRoot := filepath.Join(dir, "skills")
	if !linkDir(t, shared, skillsRoot) {
		t.Skip("cannot create a directory link on this platform without privilege")
	}

	srcA := filepath.Join(root, "src", "alpha")
	writeFile(t, filepath.Join(srcA, "SKILL.md"), "alpha content")

	d := Desired{Skills: map[string]string{"alpha": srcA}}
	st := &State{}

	_, err := SyncSkills(dir, d, st)
	if err == nil {
		t.Fatal("expected SyncSkills to refuse a linked skills dir")
	}
	if !strings.Contains(err.Error(), "refusing to manage a shared skills directory") {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(shared, "handwritten", "SKILL.md"))
	if err != nil || string(got) != "precious" {
		t.Fatalf("shared dir was disturbed: %v %q", err, got)
	}
	if entries, err := os.ReadDir(shared); err != nil || len(entries) != 1 {
		t.Fatalf("shared dir contents changed: %v %v", err, entries)
	}
}

func TestSyncSkills_MissingSourceKeepsExistingCopy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "identity")

	srcA := filepath.Join(root, "src", "alpha")
	writeFile(t, filepath.Join(srcA, "SKILL.md"), "alpha content")

	d := Desired{Skills: map[string]string{"alpha": srcA}}
	st := &State{}
	if _, err := SyncSkills(dir, d, st); err != nil {
		t.Fatalf("initial SyncSkills: %v", err)
	}

	dst := filepath.Join(dir, "skills", "alpha")
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Fatalf("initial copy missing: %v", err)
	}

	// Catalog now points at a path that doesn't exist.
	d2 := Desired{Skills: map[string]string{"alpha": filepath.Join(root, "src", "gone")}}
	if _, err := SyncSkills(dir, d2, st); err == nil {
		t.Fatal("expected an error for a missing source dir")
	}

	got, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil || string(got) != "alpha content" {
		t.Fatalf("existing managed copy was destroyed: %v %q", err, got)
	}
	if _, err := os.Stat(filepath.Join(dst, skillMarkerName)); err != nil {
		t.Fatalf("marker missing after failed sync: %v", err)
	}
}
