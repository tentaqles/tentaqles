package ignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchSyntax(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		isDir   bool
		want    bool
	}{
		// basename patterns match at any depth
		{"*.log", "a.log", false, true},
		{"*.log", "x/y/a.log", false, true},
		{"*.log", "a.log.txt", false, false},
		{"foo", "a/b/foo", false, true},
		{"foo", "a/b/foo", true, true},
		// leading slash anchors to the ignore file's directory
		{"/root.txt", "root.txt", false, true},
		{"/root.txt", "sub/root.txt", false, false},
		// a middle slash anchors too
		{"doc/frotz", "doc/frotz", false, true},
		{"doc/frotz", "a/doc/frotz", false, false},
		// trailing slash: directories only
		{"build/", "build", true, true},
		{"build/", "build", false, false},
		{"build/", "src/build", true, true},
		// leading **
		{"**/cache", "cache", true, true},
		{"**/cache", "a/b/cache", false, true},
		{"**/foo/bar", "x/foo/bar", false, true},
		{"**/foo/bar", "foo/bar", false, true},
		// middle **
		{"a/**/b", "a/b", false, true},
		{"a/**/b", "a/x/b", false, true},
		{"a/**/b", "a/x/y/b", false, true},
		{"a/**/b", "z/a/x/b", false, false},
		// trailing **
		{"logs/**", "logs/x", false, true},
		{"logs/**", "logs/x/y.txt", false, true},
		{"logs/**", "logs", true, false},
		// other runs of stars are one *
		{"a**b", "axxb", false, true},
		{"a**b", "ax/xb", false, false},
		// * and ? never cross a slash
		{"a*c", "abbc", false, true},
		{"a*/c", "ab/c", false, true},
		{"a?c", "abc", false, true},
		{"a?c", "a/c", false, false},
		// classes
		{"[abc]x.txt", "bx.txt", false, true},
		{"[abc]x.txt", "dx.txt", false, false},
		{"[!m]y.txt", "ay.txt", false, true},
		{"[!m]y.txt", "my.txt", false, false},
		{"[^m]y.txt", "my.txt", false, false},
		{"[a-c]z", "bz", false, true},
		{"[a-c]z", "dz", false, false},
		{"[]]q", "]q", false, true},
		// escapes
		{`\#hash.txt`, "#hash.txt", false, true},
		{`\!bang.txt`, "!bang.txt", false, true},
		{`a\*b`, "a*b", false, true},
		{`a\*b`, "axb", false, false},
		// trailing spaces stripped unless escaped
		{"trailing.txt   ", "trailing.txt", false, true},
		{`space\ `, "space ", false, true},
		// case-insensitive (errs toward exclusion)
		{"*.LOG", "a.log", false, true},
		// regex metacharacters are literal
		{"a+b(c).txt", "a+b(c).txt", false, true},
		{"a.b", "axb", false, false},
	}
	for _, c := range cases {
		r, err := Parse(c.pattern)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.pattern, err)
			continue
		}
		if got := r.Match(c.path, c.isDir); got != c.want {
			t.Errorf("%q vs %q (dir=%v) = %v, want %v", c.pattern, c.path, c.isDir, got, c.want)
		}
	}
}

func TestParseSkipsCommentsAndNegations(t *testing.T) {
	r, err := Parse("# comment\n\n!keep.log\n*.log\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Patterns) != 1 {
		t.Fatalf("patterns = %+v", r.Patterns)
	}
	// The negation is dropped, so keep.log stays ignored: more exclusion.
	if !r.Match("keep.log", false) {
		t.Fatal("negation un-ignored a file")
	}
}

func TestParseFailsClosed(t *testing.T) {
	for _, bad := range []string{
		"[unclosed",
		"trailing\\",
		"[[:space:]]x",
		"[z-a]",
		"/",
		"a//",
	} {
		if _, err := Parse("ok.txt\n" + bad + "\n"); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalExcludeFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	global := filepath.Join(home, "custom.gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	write(t, global, "[core]\n\texcludesFile = \"~/from-global\" ; comment\n[include]\n\tpath = inc.cfg\n")
	write(t, filepath.Join(home, "inc.cfg"), "[core]\nexcludesfile = /abs/from-include\n")
	write(t, filepath.Join(home, ".gitconfig"), "[includeIf \"gitdir:~/work/\"]\n\tpath = ~/.gitconfig-work\n")
	write(t, filepath.Join(home, ".gitconfig-work"), "[CORE]\n  ExcludesFile = ~/from-includeif\n")
	repoCfg := filepath.Join(home, "repo", ".git", "config")
	write(t, repoCfg, "[core]\n\texcludesfile = rel-ignore\n")

	got := strings.Join(GlobalExcludeFiles(repoCfg), "|")
	for _, want := range []string{
		filepath.Join(home, "from-global"),
		filepath.FromSlash("/abs/from-include"),
		filepath.Join(home, "from-includeif"),
		filepath.Join(home, "xdg", "git", "ignore"),
		filepath.Join(home, "repo", ".git", "rel-ignore"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestFindRepoWorktree(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "main", ".git")
	write(t, filepath.Join(main, "config"), "")
	wtGit := filepath.Join(main, "worktrees", "wt")
	write(t, filepath.Join(wtGit, "commondir"), "../..\n")
	wt := filepath.Join(base, "wt")
	write(t, filepath.Join(wt, ".git"), "gitdir: "+wtGit+"\n")
	sub := filepath.Join(wt, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	top, gitDir, common, ok := FindRepo(sub)
	if !ok || top != wt || gitDir != wtGit || filepath.Clean(common) != main {
		t.Fatalf("top=%s git=%s common=%s ok=%v", top, gitDir, common, ok)
	}
}
