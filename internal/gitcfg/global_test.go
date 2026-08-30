package gitcfg

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// gitGlobal isolates HOME, USERPROFILE and GIT_CONFIG_GLOBAL so these tests can
// drive a real `git config --global` without ever reading or rewriting the
// developer's own ~/.gitconfig. On Windows os.UserHomeDir() reads USERPROFILE,
// so both must be set. It returns the path of the temp global config.
func gitGlobal(t *testing.T, body string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := testutil.TempDir(t)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", h)
	}
	t.Setenv("HOME", h)
	gc := filepath.Join(h, ".gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", gc)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := os.WriteFile(gc, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return gc
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const twoIncludeIf = "[user]\n" +
	"\tname = Renato\n" +
	"\temail = me@corp.com\n" +
	"[includeIf \"gitdir:C:/repos/alpha/\"]\n" +
	"\tpath = C:/repos/alpha/.gitconfig-tentaqles\n" +
	"[includeIf \"gitdir/i:C:/repos/beta with space/\"]\n" +
	"\tpath = C:/repos/beta with space/.gitconfig-tentaqles\n" +
	"[include]\n" +
	"\tpath = ~/.gitconfig-tentaqles\n" +
	"\tpath = /nope/missing.gitconfig\n"

func TestListIncludeIf_ParsesCondAndPath(t *testing.T) {
	gitGlobal(t, twoIncludeIf)
	got, err := ListIncludeIf(RunGit)
	if err != nil {
		t.Fatal(err)
	}
	want := []IncludeIf{
		{Cond: "gitdir:C:/repos/alpha/", Path: "C:/repos/alpha/.gitconfig-tentaqles"},
		{Cond: "gitdir/i:C:/repos/beta with space/", Path: "C:/repos/beta with space/.gitconfig-tentaqles"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListIncludeIf_EmptyConfig(t *testing.T) {
	gitGlobal(t, "")
	got, err := ListIncludeIf(RunGit)
	if err != nil {
		t.Fatalf("an unset key must not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

// The point of --show-origin: includeIf blocks that live in tq's own managed
// include file are tq's, not drift, and must never be reported or removed.
func TestListIncludeIf_SkipsTqManagedIncludeFile(t *testing.T) {
	h := testutil.TempDir(t)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", h)
	}
	t.Setenv("HOME", h)
	// git --null form: <origin>\0<key>\n<value>\0 per entry.
	out := "file:" + filepath.Join(h, ".gitconfig") + "\x00includeif.gitdir:C:/repos/alpha/.path\nC:/repos/alpha/.gitconfig-tentaqles\x00" +
		"file:" + IncludeFile() + "\x00includeif.gitdir:C:/repos/beta/.path\nC:/repos/beta/.gitconfig-tentaqles\x00"
	got, err := ListIncludeIf(func(...string) (string, error) { return out, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Cond != "gitdir:C:/repos/alpha/" {
		t.Fatalf("%+v", got)
	}
}

func TestRemoveIncludeIf_RemovesKeyAndEmptySection(t *testing.T) {
	gc := gitGlobal(t, twoIncludeIf)
	inc := IncludeIf{Cond: "gitdir:C:/repos/alpha/", Path: "C:/repos/alpha/.gitconfig-tentaqles"}
	if err := RemoveIncludeIf(RunGit, inc); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, gc)
	if strings.Contains(body, "alpha") {
		t.Fatalf("alpha block survived:\n%s", body)
	}
	if !strings.Contains(body, "beta with space") {
		t.Fatalf("sibling includeIf was removed:\n%s", body)
	}
	if !strings.Contains(body, "me@corp.com") {
		t.Fatalf("unrelated section was removed:\n%s", body)
	}
	got, err := ListIncludeIf(RunGit)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestRemoveIncludeIf_KeepsSectionWithOtherKeys(t *testing.T) {
	body := "[includeIf \"gitdir:C:/repos/alpha/\"]\n" +
		"\tpath = C:/repos/alpha/.gitconfig-tentaqles\n" +
		"\tnote = keep me\n"
	gc := gitGlobal(t, body)
	inc := IncludeIf{Cond: "gitdir:C:/repos/alpha/", Path: "C:/repos/alpha/.gitconfig-tentaqles"}
	if err := RemoveIncludeIf(RunGit, inc); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, gc)
	if !strings.Contains(got, "keep me") {
		t.Fatalf("a section with remaining keys must not be removed:\n%s", got)
	}
	if strings.Contains(got, ".gitconfig-tentaqles") {
		t.Fatalf("path key survived:\n%s", got)
	}
}

func TestRemoveIncludeIf_MissingIsNotAnError(t *testing.T) {
	gitGlobal(t, "")
	if err := RemoveIncludeIf(RunGit, IncludeIf{Cond: "gitdir:C:/repos/nope/", Path: "C:/x"}); err != nil {
		t.Fatalf("removing an absent includeIf must be a no-op: %v", err)
	}
}

func TestListIncludes(t *testing.T) {
	gitGlobal(t, twoIncludeIf)
	got, err := ListIncludes(RunGit)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"~/.gitconfig-tentaqles", "/nope/missing.gitconfig"}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("include %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// The value handed to git --unset is a regex; an unanchored, unescaped path
// would take neighbouring includes with it.
func TestRemoveInclude_ExactMatchOnly(t *testing.T) {
	gitGlobal(t, "[include]\n\tpath = /a/.gitconfig-tentaqles\n\tpath = /a/.gitconfig-tentaqles.bak\n")
	if err := RemoveInclude(RunGit, "/a/.gitconfig-tentaqles"); err != nil {
		t.Fatal(err)
	}
	got, err := ListIncludes(RunGit)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "/a/.gitconfig-tentaqles.bak" {
		t.Fatalf("%+v", got)
	}
}

func TestGetGlobalAndUnsetGlobal(t *testing.T) {
	gitGlobal(t, "")
	if _, present, err := GetGlobal(RunGit, "user.email"); err != nil || present {
		t.Fatalf("present=%v err=%v", present, err)
	}
	if _, err := RunGit("config", "--global", "user.email", "me@corp.com"); err != nil {
		t.Fatal(err)
	}
	v, present, err := GetGlobal(RunGit, "user.email")
	if err != nil || !present || v != "me@corp.com" {
		t.Fatalf("v=%q present=%v err=%v", v, present, err)
	}
	if err := UnsetGlobal(RunGit, "user.email"); err != nil {
		t.Fatal(err)
	}
	if _, present, _ := GetGlobal(RunGit, "user.email"); present {
		t.Fatal("user.email still present after UnsetGlobal")
	}
}

func TestUnsetGlobal_MissingIsNotAnError(t *testing.T) {
	gitGlobal(t, "")
	if err := UnsetGlobal(RunGit, "user.email"); err != nil {
		t.Fatalf("unsetting an absent key must be a no-op: %v", err)
	}
}

func TestParseUserSection(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, body, wantName, wantEmail string
	}{
		{"plain", "# managed by tq\n[user]\n\tname = Renato\n\temail = me@corp.com\n", "Renato", "me@corp.com"},
		{"quoted", "[user]\n\tname = \"Renato D\"\n\temail = \"me@corp.com\"\n", "Renato D", "me@corp.com"},
		{"comment", "[user]\n\temail = me@corp.com # work\n", "", "me@corp.com"},
		{"crlf", "[user]\r\n\tname = Renato\r\n\temail = me@corp.com\r\n", "Renato", "me@corp.com"},
		{"other-sections", "[core]\n\tsshCommand = ssh -i k\n[user]\n\temail = me@corp.com\n[alias]\n\tname = nope\n", "", "me@corp.com"},
		{"no-user", "[core]\n\tautocrlf = true\n", "", ""},
		{"case-insensitive", "[USER]\n\tEmail = me@corp.com\n", "", "me@corp.com"},
		{"subsection-ignored", "[user \"alt\"]\n\temail = other@corp.com\n[user]\n\temail = me@corp.com\n", "", "me@corp.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(dir, c.name)
			if err := os.WriteFile(p, []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			name, email, err := ParseUserSection(p)
			if err != nil {
				t.Fatal(err)
			}
			if name != c.wantName || email != c.wantEmail {
				t.Fatalf("name=%q email=%q, want %q / %q", name, email, c.wantName, c.wantEmail)
			}
		})
	}
}

func TestParseUserSection_MissingFile(t *testing.T) {
	_, _, err := ParseUserSection(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
