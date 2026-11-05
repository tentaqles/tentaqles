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
	for _, bad := range []string{"", "   ", ".", "..", "bash", "pwsh.exe", "node", "claude", "codex.exe", "-bash", "a/b", `a\b`, strings.Repeat("x", 129),
		// root and service processes, and runtimes
		"init", "systemd", "launchd", "svchost.exe", "services.exe", "wininit.exe", "smss.exe", "csrss.exe", "cron", "crond", "atd",
		"python", "python3", "perl", "ruby", "java"} {
		if n, err := ValidateAnchor(bad); err == nil {
			t.Errorf("ValidateAnchor(%q) = %q, want an error", bad, n)
		}
	}
	for name, want := range map[string]string{"systemd": "root process", "svchost": "root or service process", "python": "runtime", "java": "JetBrains IDE"} {
		if _, err := ValidateAnchor(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateAnchor(%q) must say why (%q): %v", name, want, err)
		}
	}
	// sshd and explorer stay built-in anchors, and a built-in name may be
	// added again without harm.
	for _, ok := range []string{"sshd", "explorer.exe", "kitty"} {
		if _, err := ValidateAnchor(ok); err != nil {
			t.Errorf("ValidateAnchor(%q): %v", ok, err)
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

// A user anchor goes through the same qualifiers as a built-in one: a name
// that needs its command line (systemd, init, java) is never granted by
// name, and a hand-edited anchors.yaml cannot hold a root, a runtime, a
// shell or a harness at all.
func TestUserAnchorsNeverBypassQualifiers(t *testing.T) {
	home(t)
	if err := os.WriteFile(UserAnchorsPath(), []byte("anchors: [init, systemd, java, python, bash, claude, foo-term, launchd]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadUserAnchors()
	if err != nil || strings.Join(got, ",") != "foo-term" {
		t.Fatalf("a hand-edited file keeps only a terminal name: %v %v", got, err)
	}
	// Even set directly (no file at all), the qualifier decides.
	prev := userAnchors
	userAnchors = set("init", "systemd", "java", "foo-term")
	t.Cleanup(func() { userAnchors = prev })
	for _, c := range []struct {
		name, cmdline string
		known         bool
		want          Kind
	}{
		{"init", "/sbin/init", true, KindNeutral},
		{"init", "/init", true, KindAnchor},
		{"init", "", false, KindUnknown},
		{"systemd", "/sbin/init splash", true, KindNeutral},
		{"systemd", "/usr/lib/systemd/systemd --user", true, KindAnchor},
		{"systemd", "", false, KindUnknown},
		{"java", "java -jar app.jar", true, KindNeutral},
		{"java", "/opt/idea/jbr/bin/java com.intellij.idea.Main", true, KindAnchor},
		{"foo-term", "", false, KindAnchor},
		{"foo-term", "/opt/foo-term/foo-term", true, KindAnchor},
	} {
		if got := Classify(c.name, c.cmdline, c.known); got != c.want {
			t.Errorf("Classify(%q, %q, %v) = %v, want %v", c.name, c.cmdline, c.known, got, c.want)
		}
	}
}

// home is human without the detector seams: an isolated TQ_HOME only.
func home(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("TQ_HOME", h)
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
}
