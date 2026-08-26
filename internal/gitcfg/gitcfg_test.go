package gitcfg

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func home(t *testing.T) string {
	h := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", h)
	} else {
		t.Setenv("HOME", h)
	}
	return h
}

func TestWriteWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := WriteWorkspace(root, "Maria", "m@acme.com"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(WorkspaceFile(root))
	s := string(b)
	for _, want := range []string{"[user]", "name = Maria", "email = m@acme.com", "useConfigOnly = true"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
}

func TestSync_IdempotentAndPrunes(t *testing.T) {
	home(t)
	a, b := t.TempDir(), t.TempDir()
	if err := Sync([]string{b, a}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(IncludeFile())
	if err := Sync([]string{a, b}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(IncludeFile())
	if string(first) != string(second) {
		t.Fatalf("not idempotent/sorted:\n%s\n---\n%s", first, second)
	}
	if !strings.Contains(string(first), `[includeIf "gitdir:`+filepath.ToSlash(a)+`/"]`) {
		t.Fatalf("missing includeIf for a:\n%s", first)
	}
	Sync([]string{a})
	third, _ := os.ReadFile(IncludeFile())
	if strings.Contains(string(third), filepath.ToSlash(b)) {
		t.Fatal("removed root must be pruned")
	}
}

func TestEnsureGlobal_AddsOnceAndSetsUseConfigOnly(t *testing.T) {
	home(t)
	var calls []string
	includes := ""
	fake := func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if strings.Contains(strings.Join(args, " "), "--get-all include.path") {
			return includes, nil
		}
		if len(args) >= 4 && args[2] == "--add" {
			includes += args[4] + "\n"
		}
		return "", nil
	}
	if err := EnsureGlobal(fake); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGlobal(fake); err != nil {
		t.Fatal(err)
	}
	adds := 0
	for _, c := range calls {
		if strings.Contains(c, "--add include.path") {
			adds++
		}
	}
	if adds != 1 {
		t.Fatalf("include.path added %d times: %v", adds, calls)
	}
	if !strings.Contains(strings.Join(calls, "|"), "user.useConfigOnly true") {
		t.Fatalf("%v", calls)
	}
}
