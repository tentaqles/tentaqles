package humanonly

import (
	"os"
	"strings"
	"testing"
)

func TestUserAnchorsStore(t *testing.T) {
	human(t)
	if got, err := LoadUserAnchors(); err != nil || len(got) != 0 {
		t.Fatalf("no file is an empty list: %v %v", got, err)
	}
	n, added, err := AddUserAnchor("Foo-Term.exe")
	if err != nil || !added || n != "foo-term" {
		t.Fatalf("add: %q %v %v", n, added, err)
	}
	if _, added, err := AddUserAnchor("FOO-TERM"); err != nil || added {
		t.Fatalf("a second add is a no-op: %v %v", added, err)
	}
	if _, _, err := AddUserAnchor("bar-term"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadUserAnchors()
	if err != nil || strings.Join(got, ",") != "bar-term,foo-term" {
		t.Fatalf("list: %v %v", got, err)
	}
	fi, err := os.Stat(UserAnchorsPath())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 && os.Getenv("OS") != "Windows_NT" {
		t.Fatalf("anchors.yaml must be private: %v", fi.Mode())
	}
	n, removed, err := RemoveUserAnchor("Foo-Term.exe")
	if err != nil || !removed || n != "foo-term" {
		t.Fatalf("remove: %q %v %v", n, removed, err)
	}
	if _, removed, err := RemoveUserAnchor("foo-term"); err != nil || removed {
		t.Fatalf("a second remove is a no-op: %v %v", removed, err)
	}
	if got, _ := LoadUserAnchors(); strings.Join(got, ",") != "bar-term" {
		t.Fatalf("after remove: %v", got)
	}
}

func TestValidateAnchorRefusesShellsHarnessesAndPaths(t *testing.T) {
	for _, bad := range []string{"", "   ", ".", "..", "bash", "pwsh.exe", "node", "claude", "codex.exe", "-bash", "a/b", `a\b`, strings.Repeat("x", 129)} {
		if n, err := ValidateAnchor(bad); err == nil {
			t.Errorf("ValidateAnchor(%q) = %q, want an error", bad, n)
		}
	}
	for in, want := range map[string]string{"Foo-Term.exe": "foo-term", "KITTY": "kitty", " my term ": "my term"} {
		if n, err := ValidateAnchor(in); err != nil || n != want {
			t.Errorf("ValidateAnchor(%q) = %q %v, want %q", in, n, err, want)
		}
	}
}

// Detect reads anchors.yaml before each walk: an added terminal turns a
// refused chain into a human's, and a removed one refuses it again, with the
// add command in the reason.
func TestDetectHonoursUserAnchors(t *testing.T) {
	human(t)
	ParentOf = fakeTree(map[int]fakeProc{
		selfPID(): {90, "tq.exe", "tq", false, false},
		90:        {80, "pwsh.exe", "pwsh", false, false},
		80:        {70, "Foo-Term.exe", `"C:\Program Files\FooTerm\Foo-Term.exe"`, false, false},
		70:        {4242, "svchost.exe", "", false, false},
	})
	r := Detect()
	if r == nil || r.Detector != DetectorProcessTree || !strings.Contains(r.Reason, "tq settings anchors add foo-term") {
		t.Fatalf("an unlisted terminal refuses with the add line: %+v", r)
	}
	if _, _, err := AddUserAnchor("Foo-Term.exe"); err != nil {
		t.Fatal(err)
	}
	if r := Detect(); r != nil {
		t.Fatalf("an added terminal passes: %+v", r)
	}
	if _, _, err := RemoveUserAnchor("foo-term"); err != nil {
		t.Fatal(err)
	}
	if r := Detect(); r == nil {
		t.Fatal("a removed terminal refuses again")
	}
	// A broken file adds nothing and never passes a chain.
	if err := os.WriteFile(UserAnchorsPath(), []byte("anchors: [foo-term"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := Detect(); r == nil {
		t.Fatal("an unreadable anchors.yaml must not pass the chain")
	}
}
