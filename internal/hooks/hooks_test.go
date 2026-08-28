package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestStatusOf_MissingFile(t *testing.T) {
	p := tempProfiles(t, "bash")
	st := StatusOf("bash", p)
	if st.State != "missing" {
		t.Fatalf("expected missing, got %s", st.State)
	}
}
