package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func tempProfiles(t *testing.T, shells ...Shell) Profiles {
	t.Helper()
	dir := t.TempDir()
	p := Profiles{}
	for _, sh := range shells {
		p[sh] = filepath.Join(dir, string(sh)+"-profile")
	}
	return p
}

func TestBlock_Contents(t *testing.T) {
	for _, sh := range Shells {
		b := Block(sh)
		if !strings.Contains(b, "# >>> tq >>>") {
			t.Errorf("%s: missing start marker", sh)
		}
		if !strings.Contains(b, "# <<< tq <<<") {
			t.Errorf("%s: missing end marker", sh)
		}
		if !strings.Contains(b, "tq activate "+string(sh)) {
			t.Errorf("%s: missing activate call", sh)
		}
		if !strings.Contains(b, "TQ_ENABLED") {
			t.Errorf("%s: missing TQ_ENABLED guard", sh)
		}
		if !strings.HasSuffix(b, "\n") {
			t.Errorf("%s: block should end with trailing newline", sh)
		}
	}
}

func TestInstall_CreatesAndIsIdempotent(t *testing.T) {
	p := tempProfiles(t, "bash")
	st, err := Install("bash", p)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "installed" {
		t.Fatalf("expected installed, got %s", st.State)
	}
	data, err := os.ReadFile(p["bash"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# >>> tq >>>") {
		t.Fatal("block not written")
	}

	st2, err := Install("bash", p)
	if err != nil {
		t.Fatal(err)
	}
	if st2.State != "installed" {
		t.Fatalf("expected installed on 2nd call, got %s", st2.State)
	}
	data2, err := os.ReadFile(p["bash"])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(data2) {
		t.Fatal("second install call changed file contents")
	}
}

func TestInstall_DetectsUnmanaged(t *testing.T) {
	p := tempProfiles(t, "pwsh")
	unmanaged := "Write-Host 'hi'\ntq activate pwsh | Out-String | Invoke-Expression\n"
	if err := os.WriteFile(p["pwsh"], []byte(unmanaged), 0644); err != nil {
		t.Fatal(err)
	}
	st, err := Install("pwsh", p)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "present (unmanaged)" {
		t.Fatalf("expected present (unmanaged), got %s", st.State)
	}
	data, err := os.ReadFile(p["pwsh"])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != unmanaged {
		t.Fatal("file was modified when unmanaged activate line present")
	}
}

func TestRemove_OnlyOurBlock(t *testing.T) {
	p := tempProfiles(t, "zsh")
	other := "# my custom stuff\nexport FOO=bar\n"
	if err := os.WriteFile(p["zsh"], []byte(other), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("zsh", p); err != nil {
		t.Fatal(err)
	}
	st, err := Remove("zsh", p)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "missing" {
		t.Fatalf("expected missing after remove, got %s", st.State)
	}
	data, err := os.ReadFile(p["zsh"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "export FOO=bar") {
		t.Fatal("other content was removed")
	}
	if strings.Contains(string(data), "# >>> tq >>>") {
		t.Fatal("our block still present after remove")
	}

	// Removing again is a no-op.
	st2, err := Remove("zsh", p)
	if err != nil {
		t.Fatal(err)
	}
	if st2.State != "missing" {
		t.Fatalf("expected missing on second remove, got %s", st2.State)
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	bashProfile := filepath.Join(dir, "bashrc")
	if err := os.WriteFile(bashProfile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	p := Profiles{
		"bash": bashProfile,
		"zsh":  filepath.Join(dir, "zshrc-missing"),
		"fish": filepath.Join(dir, "fish-missing"),
		"pwsh": filepath.Join(dir, "pwsh-missing"),
	}
	lookPath := func(name string) (string, error) {
		if name == "fish" {
			return "/usr/bin/fish", nil
		}
		return "", os.ErrNotExist
	}
	shells := Detect(p, lookPath)
	found := map[Shell]bool{}
	for _, s := range shells {
		found[s] = true
	}
	if !found["bash"] {
		t.Error("expected bash detected via existing profile")
	}
	if !found["fish"] {
		t.Error("expected fish detected via lookPath")
	}
	if found["zsh"] {
		t.Error("did not expect zsh detected")
	}
	if found["pwsh"] {
		t.Error("did not expect pwsh detected")
	}
}

func TestStatusOf_NoProfile(t *testing.T) {
	st := StatusOf("bash", Profiles{})
	if st.State != "no profile" {
		t.Fatalf("expected no profile, got %s", st.State)
	}
}

func TestStatusOf_NoProfileForAbsentFile(t *testing.T) {
	p := tempProfiles(t, "bash")
	st := StatusOf("bash", p)
	if st.State != "no profile" {
		t.Fatalf("expected no profile for a profile path that does not exist on disk, got %s", st.State)
	}
}

func TestStatusOf_MissingWhenFileExistsWithoutBlock(t *testing.T) {
	p := tempProfiles(t, "bash")
	if err := os.WriteFile(p["bash"], []byte("export FOO=bar\n"), 0644); err != nil {
		t.Fatal(err)
	}
	st := StatusOf("bash", p)
	if st.State != "missing" {
		t.Fatalf("expected missing for an existing file without our block, got %s", st.State)
	}
}

func TestInstall_MatchesCRLF(t *testing.T) {
	p := tempProfiles(t, "pwsh")
	if err := os.WriteFile(p["pwsh"], []byte("Write-Host 'hi'\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	st, err := Install("pwsh", p)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "installed" {
		t.Fatalf("expected installed, got %s", st.State)
	}
	data, err := os.ReadFile(p["pwsh"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\r\n") {
		t.Fatal("expected CRLF-preserving write")
	}
	if strings.Count(string(data), "\r\n") < strings.Count(string(data), "\n") {
		t.Fatalf("expected all newlines in written content to be CRLF, got: %q", string(data))
	}
}

func TestInstall_CreatesBackupOnce(t *testing.T) {
	p := tempProfiles(t, "bash")
	original := "export FOO=1\n"
	if err := os.WriteFile(p["bash"], []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("bash", p); err != nil {
		t.Fatal(err)
	}
	bak := p["bash"] + ".tq-backup"
	got, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("no backup created: %v", err)
	}
	if string(got) != original {
		t.Fatalf("backup = %q, want %q", got, original)
	}

	// A later modification must not overwrite the pristine backup.
	if _, err := Remove("bash", p); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("bash", p); err != nil {
		t.Fatal(err)
	}
	got2, err := os.ReadFile(bak)
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != original {
		t.Fatalf("backup overwritten: %q, want %q", got2, original)
	}
}

func TestInstall_NoTmpLeftover(t *testing.T) {
	p := tempProfiles(t, "bash")
	if _, err := Install("bash", p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p["bash"] + ".tq-tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind (err=%v)", err)
	}
	if _, err := Remove("bash", p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p["bash"] + ".tq-tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind after remove (err=%v)", err)
	}
}

func TestStatusOf_CorruptSingleMarker(t *testing.T) {
	for _, marker := range []string{"# >>> tq >>>", "# <<< tq <<<"} {
		p := tempProfiles(t, "bash")
		if err := os.WriteFile(p["bash"], []byte("export FOO=1\n"+marker+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if st := StatusOf("bash", p); st.State != "corrupt" {
			t.Fatalf("marker %q: state = %q, want corrupt", marker, st.State)
		}
		if _, err := Install("bash", p); err == nil {
			t.Fatalf("marker %q: Install should refuse a corrupt profile", marker)
		}
	}
}

func TestRemove_RepairsSingleMarker(t *testing.T) {
	// Only the start marker: everything from it to EOF goes.
	p := tempProfiles(t, "bash")
	if err := os.WriteFile(p["bash"], []byte("export FOO=1\n# >>> tq >>>\nsome tq line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Remove("bash", p)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "repaired" {
		t.Fatalf("state = %q, want repaired", st.State)
	}
	data, err := os.ReadFile(p["bash"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tq") {
		t.Fatalf("partial block not removed:\n%s", data)
	}
	if !strings.Contains(string(data), "export FOO=1") {
		t.Fatalf("user content lost:\n%s", data)
	}

	// Only the end marker: just that line goes.
	p2 := tempProfiles(t, "zsh")
	if err := os.WriteFile(p2["zsh"], []byte("export A=1\n# <<< tq <<<\nexport B=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st2, err := Remove("zsh", p2)
	if err != nil {
		t.Fatal(err)
	}
	if st2.State != "repaired" {
		t.Fatalf("state = %q, want repaired", st2.State)
	}
	data2, err := os.ReadFile(p2["zsh"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data2), "<<< tq") {
		t.Fatalf("end marker not removed:\n%s", data2)
	}
	if !strings.Contains(string(data2), "export A=1") || !strings.Contains(string(data2), "export B=2") {
		t.Fatalf("user content lost:\n%s", data2)
	}
}

// utf16le encodes s as UTF-16 little-endian with a BOM, the way Windows
// PowerShell 5.1 writes $PROFILE.
func utf16le(s string) []byte {
	out := []byte{0xFF, 0xFE}
	for _, c := range utf16.Encode([]rune(s)) {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

func TestInstall_UTF16LEProfile(t *testing.T) {
	p := tempProfiles(t, "powershell")
	original := "Write-Host hi\r\n"
	if err := os.WriteFile(p["powershell"], utf16le(original), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Install("powershell", p)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "installed" {
		t.Fatalf("state = %q, want installed", st.State)
	}
	raw, err := os.ReadFile(p["powershell"])
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xFE {
		t.Fatalf("UTF-16LE BOM not preserved: % x", raw[:min(4, len(raw))])
	}
	decoded, enc := decodeProfile(raw)
	if enc != encUTF16LE {
		t.Fatalf("encoding = %v, want UTF-16LE", enc)
	}
	if !strings.Contains(decoded, "# >>> tq >>>") || !strings.Contains(decoded, "# <<< tq <<<") {
		t.Fatalf("block missing from decoded profile:\n%q", decoded)
	}
	if !strings.Contains(decoded, "Write-Host hi") {
		t.Fatalf("user content lost:\n%q", decoded)
	}

	// And removing gets us back to the original bytes.
	if _, err := Remove("powershell", p); err != nil {
		t.Fatal(err)
	}
	raw2, err := os.ReadFile(p["powershell"])
	if err != nil {
		t.Fatal(err)
	}
	dec2, enc2 := decodeProfile(raw2)
	if enc2 != encUTF16LE {
		t.Fatalf("encoding after remove = %v, want UTF-16LE", enc2)
	}
	if strings.Contains(dec2, "tq >>>") {
		t.Fatalf("block not removed:\n%q", dec2)
	}
}

func TestInstall_PreservesUTF8BOM(t *testing.T) {
	p := tempProfiles(t, "bash")
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte("export FOO=1\n")...)
	if err := os.WriteFile(p["bash"], raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("bash", p); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p["bash"])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 || got[0] != 0xEF || got[1] != 0xBB || got[2] != 0xBF {
		t.Fatalf("UTF-8 BOM not preserved: % x", got[:min(4, len(got))])
	}
	if strings.Count(string(got), "\xEF\xBB\xBF") != 1 {
		t.Fatalf("BOM duplicated or lost:\n% x", got)
	}
	if !strings.Contains(string(got), "# >>> tq >>>") {
		t.Fatalf("block not written:\n%s", got)
	}
}

func TestDetect_SkipsShellsWithNoProfileMapping(t *testing.T) {
	// Only bash has a mapping; every shell resolves via lookPath, so any
	// unmapped shell that shows up came from the missing-mapping path.
	p := tempProfiles(t, "bash")
	always := func(string) (string, error) { return "/usr/bin/x", nil }
	got := Detect(p, always)
	if len(got) != 1 || got[0] != "bash" {
		t.Fatalf("Detect = %v, want [bash]", got)
	}
}

func TestWriteProfile_ReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile")
	if err := os.WriteFile(path, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeProfile(path, []byte("new content"), 0o644); err != nil {
		t.Fatalf("writeProfile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Errorf("path content = %q, want %q", got, "new content")
	}

	if _, err := os.Stat(path + tmpSuffix); !os.IsNotExist(err) {
		t.Errorf("tmp file should not remain, stat err = %v", err)
	}
	if _, err := os.Stat(path + ".tq-prev"); !os.IsNotExist(err) {
		t.Errorf(".tq-prev file should not remain, stat err = %v", err)
	}
}

func TestWriteProfile_RestoresOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile")
	if err := os.WriteFile(path, []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}

	origGOOS := runtime.GOOS
	_ = origGOOS
	origRename := renameFn
	defer func() { renameFn = origRename }()

	tmp := path + tmpSuffix
	prev := path + ".tq-prev"

	callCount := 0
	renameFn = func(oldpath, newpath string) error {
		callCount++
		switch {
		case oldpath == tmp && newpath == path && callCount == 1:
			// Simulate Windows: first rename attempt fails because dest exists.
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: os.ErrExist}
		case oldpath == path && newpath == prev:
			return os.Rename(oldpath, newpath)
		case oldpath == tmp && newpath == path:
			// Second rename attempt (after moving dest aside) also fails.
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: os.ErrExist}
		case oldpath == prev && newpath == path:
			return os.Rename(oldpath, newpath)
		default:
			return os.Rename(oldpath, newpath)
		}
	}

	if runtime.GOOS != "windows" {
		// Force the Windows branch to run under test regardless of host OS
		// by only proceeding if a destination exists; the writeProfile
		// windows-only guard is bypassed via GOOS check inside the function,
		// so this test is only meaningful on windows. Skip elsewhere.
		t.Skip("writeProfile fallback path is windows-only")
	}

	err := writeProfile(path, []byte("new content"), 0o644)
	if err == nil {
		t.Fatal("expected error from writeProfile, got nil")
	}
	if !strings.Contains(err.Error(), tmp) {
		t.Errorf("error should name tmp path %q, got: %v", tmp, err)
	}

	got, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != "original content" {
		t.Errorf("path content = %q, want original content restored", got)
	}

	if _, serr := os.Stat(tmp); serr != nil {
		t.Errorf("tmp file should still exist for manual recovery, stat err = %v", serr)
	}
}
