package ignore

import (
	"os"
	"path/filepath"
	"runtime"
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

// isolatedHome points every config location at an empty temp home.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig-global"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

func TestGlobalExcludeFiles(t *testing.T) {
	home := isolatedHome(t)
	global := filepath.Join(home, ".gitconfig-global")
	write(t, global, "[core]\n\texcludesFile = \"~/from-global\" ; comment\n[include]\n\tpath = inc.cfg\n")
	write(t, filepath.Join(home, "inc.cfg"), "[core]\nexcludesfile = /abs/from-include\n")
	write(t, filepath.Join(home, ".gitconfig"), "[includeIf \"gitdir:~/work/\"]\n\tpath = ~/.gitconfig-work\n")
	write(t, filepath.Join(home, ".gitconfig-work"), "[CORE]\n  ExcludesFile = ~/from-includeif\n")
	repoCfg := filepath.Join(home, "repo", ".git", "config")
	write(t, repoCfg, "[core] excludesfile = rel-ignore\n")

	files, err := GlobalExcludeFiles(repoCfg)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(files, "|")
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

func TestGlobalExcludeFilesIncludeCycleIsFine(t *testing.T) {
	home := isolatedHome(t)
	write(t, filepath.Join(home, ".gitconfig"), "[include]\n\tpath = b.cfg\n")
	write(t, filepath.Join(home, "b.cfg"), "[core]\n\texcludesfile = ~/cyc\n[include]\n\tpath = .gitconfig\n")
	files, err := GlobalExcludeFiles()
	if err != nil || !strings.Contains(strings.Join(files, "|"), filepath.Join(home, "cyc")) {
		t.Fatalf("files=%v err=%v", files, err)
	}
}

// TestGlobalExcludeFilesFailClosed: every way a config can exist but not
// be fully applied is an error, never a silently shorter list.
func TestGlobalExcludeFilesFailClosed(t *testing.T) {
	deep := func(home string) {
		for i := 0; i < 13; i++ {
			write(t, filepath.Join(home, "inc"+string(rune('a'+i))+".cfg"), "[include]\n\tpath = inc"+string(rune('a'+i+1))+".cfg\n")
		}
		write(t, filepath.Join(home, ".gitconfig"), "[include]\n\tpath = inca.cfg\n")
	}
	cases := map[string]func(home string){
		"config is a directory": func(home string) {
			if err := os.MkdirAll(filepath.Join(home, ".gitconfig"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"config not UTF-8":         func(home string) { write(t, filepath.Join(home, ".gitconfig"), "[core]\n\texcludesfile = \xff\xfe\n") },
		"malformed section header": func(home string) { write(t, filepath.Join(home, ".gitconfig"), "[core\n\texcludesfile = x\n") },
		"line continuation":        func(home string) { write(t, filepath.Join(home, ".gitconfig"), "[core]\n\texcludesfile = a\\\n") },
		"unknown escape": func(home string) {
			write(t, filepath.Join(home, ".gitconfig"), "[core]\n\texcludesfile = \"C:\\Users\\x\"\n")
		},
		"unterminated quote": func(home string) { write(t, filepath.Join(home, ".gitconfig"), "[core]\n\texcludesfile = \"abc\n") },
		"excludesFile ~user": func(home string) {
			write(t, filepath.Join(home, ".gitconfig"), "[core]\n\texcludesfile = ~bob/ignore\n")
		},
		"excludesFile %(prefix)": func(home string) {
			write(t, filepath.Join(home, ".gitconfig"), "[core]\n\texcludesfile = %(prefix)/etc/ignore\n")
		},
		"include ~user": func(home string) {
			write(t, filepath.Join(home, ".gitconfig"), "[include]\n\tpath = ~bob/.gitconfig\n")
		},
		"include points at directory": func(home string) {
			write(t, filepath.Join(home, ".gitconfig"), "[include]\n\tpath = incdir\n")
			if err := os.MkdirAll(filepath.Join(home, "incdir"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"include chain too deep": deep,
		"too large": func(home string) {
			write(t, filepath.Join(home, ".gitconfig"), "# "+strings.Repeat("x", maxSourceBytes)+"\n")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			home := isolatedHome(t)
			setup(home)
			if files, err := GlobalExcludeFiles(); err == nil {
				t.Fatalf("accepted: %v", files)
			}
		})
	}
	t.Run("~ with no home", func(t *testing.T) {
		home := isolatedHome(t)
		cfg := filepath.Join(home, "explicit.cfg")
		write(t, cfg, "[core]\n\texcludesfile = ~/ignore\n")
		t.Setenv("GIT_CONFIG_GLOBAL", cfg)
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("home", "")
		if _, err := GlobalExcludeFiles(); err == nil {
			t.Fatal("unresolvable ~ accepted")
		}
	})
}

func TestLoadRulesFailClosed(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadRules(filepath.Join(dir, "missing"), true); err != nil {
		t.Fatalf("a missing file must be fine: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "isdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "badutf"), "*.log\n\xff\n")
	write(t, filepath.Join(dir, "big"), strings.Repeat("a\n", maxSourceBytes))
	write(t, filepath.Join(dir, "unparsable"), "[x\n")
	for _, name := range []string{"isdir", "badutf", "big", "unparsable"} {
		if _, err := LoadRules(filepath.Join(dir, name), true); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if runtime.GOOS != "windows" {
		p := filepath.Join(dir, "noperm")
		write(t, p, "*.log\n")
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(p, 0o644) })
		if _, err := os.ReadFile(p); err == nil {
			t.Log("running with privileges that bypass chmod; skipping the unreadable case")
		} else if _, err := LoadRules(p, true); err == nil {
			t.Error("unreadable file accepted")
		}
	}
	if err := os.Symlink(filepath.Join(dir, "unparsable"), filepath.Join(dir, "link")); err == nil {
		if _, err := LoadRules(filepath.Join(dir, "link"), true); err == nil {
			t.Error("symlinked in-tree ignore file accepted")
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
	r, found, err := FindRepo(sub)
	if err != nil || !found || r.Top != wt || r.GitDir != wtGit || filepath.Clean(r.CommonDir) != main {
		t.Fatalf("repo=%+v found=%v err=%v", r, found, err)
	}
}

func TestFindRepoBrokenPointersFailClosed(t *testing.T) {
	cases := map[string]func(top string){
		"gitdir to nowhere": func(top string) { write(t, filepath.Join(top, ".git"), "gitdir: "+filepath.Join(top, "nope")+"\n") },
		"no gitdir line":    func(top string) { write(t, filepath.Join(top, ".git"), "garbage\n") },
		"empty gitdir":      func(top string) { write(t, filepath.Join(top, ".git"), "gitdir:\n") },
		"gitdir is a file": func(top string) {
			write(t, filepath.Join(top, "f"), "x")
			write(t, filepath.Join(top, ".git"), "gitdir: f\n")
		},
		"commondir to nowhere": func(top string) {
			write(t, filepath.Join(top, "gd", "commondir"), "../missing\n")
			write(t, filepath.Join(top, ".git"), "gitdir: gd\n")
		},
		"commondir is a directory": func(top string) {
			if err := os.MkdirAll(filepath.Join(top, "gd", "commondir"), 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(top, ".git"), "gitdir: gd\n")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			top := t.TempDir()
			setup(top)
			if r, _, err := FindRepo(top); err == nil {
				t.Fatalf("accepted: %+v", r)
			}
		})
	}
	// Plain directory outside any repo: not found, no error.
	if _, found, err := FindRepo(t.TempDir()); err != nil || found {
		t.Logf("temp dir is inside a repo here (found=%v err=%v)", found, err)
	}
}
