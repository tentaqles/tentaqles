package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// isolateGitConfig points every git config and excludes location at an
// empty temp home, so the developer's own ~/.gitconfig never changes what a
// test sees. Returns that home.
func isolateGitConfig(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

// walked lists every file walkFiles would read, plus its stats.
func walked(t *testing.T, root string) ([]string, walkStats) {
	t.Helper()
	var out []string
	stats, err := walkFiles(root, func(rel string, _ []byte) bool {
		out = append(out, rel)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out, stats
}

func gitIn(t *testing.T, dir string, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out.String(), code
}

// TestExploreIgnoreNeverLessThanGit is a differential test against real
// git on a trusted fixture: every file `git check-ignore` reports as
// ignored must never be yielded by the walker. The walker may exclude more
// (negations, deny list, case), never less. Explore itself never runs git.
func TestExploreIgnoreNeverLessThanGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := isolateGitConfig(t)
	writeTree(t, home, map[string]string{
		".gitconfig":    "[core]\n\texcludesFile = ~/global-ignore\n",
		"global-ignore": "*.swp\nglobalonly.txt\n/global-anchored.txt\n",
	})
	root := testutil.TempDir(t)
	if _, code := gitIn(t, root, "", "init", "-q"); code != 0 {
		t.Fatal("git init failed")
	}
	files := []string{
		"src/main.go", "plain.md", "#hash.txt", "!bang.txt",
		"a.log", "keep.log", "sub/x.log",
		"rootonly.txt", "sub/rootonly.txt",
		"build/o.txt", "src/build/o.txt",
		"cache/c.txt", "a/b/cache/c.txt",
		"docs/draft.md", "docs/x/y/draft.md", "docs/x/other.md",
		"logs/l.txt", "logs/deep/l.txt",
		"foo/bar.txt", "foo/q/bar.txt", "foo/q/baz.txt",
		"abc.txt", "aXc.txt", "a-c.txt",
		"bx.txt", "dx.txt", "ay.txt", "my.txt",
		"trailing.txt", "t.tmp",
		"deep/nested/n.txt", "deep/n.txt", "other/deep/nested/n.txt",
		"out1/o.txt", "outer.txt",
		"sub/local.cfg", "sub/inner/local.cfg",
		"sub/anchored.txt", "sub/inner/anchored.txt", "anchored.txt",
		"sub/nested/a/x.txt", "sub/nested/x.txt", "sub/inner/only-inner.txt", "only-inner.txt",
		"excluded-by-info.txt", "sub/excluded-by-info.txt",
		"info-anchored/i.txt", "sub/info-anchored/i.txt",
		"f.swp", "sub/globalonly.txt", "global-anchored.txt", "sub/global-anchored.txt",
		"weird [x].txt", "space .txt",
	}
	tree := map[string]string{
		".gitignore": strings.Join([]string{
			"# a comment",
			`\#hash.txt`,
			`\!bang.txt`,
			"*.log",
			"!keep.log",
			"/rootonly.txt",
			"build/",
			"**/cache",
			"docs/**/draft.md",
			"logs/**",
			"foo/**/bar.txt",
			"a?c.txt",
			"[abc]x.txt",
			"[!m]y.txt",
			"trailing.txt   ",
			"*.tmp",
			"deep/nested/",
			"out*/",
			`weird \[x\].txt`,
			`space\ .txt`,
		}, "\n") + "\n",
		"sub/.gitignore":       "local.cfg\n/anchored.txt\nnested/**/x.txt\n",
		"sub/inner/.gitignore": "!local.cfg\nonly-inner.txt\n",
		".git/info/exclude":    "excluded-by-info.txt\n/info-anchored/\n",
	}
	for _, f := range files {
		tree[f] = "fluxgate\n"
	}
	writeTree(t, root, tree)

	out, code := gitIn(t, root, strings.Join(files, "\n")+"\n", "check-ignore", "--no-index", "--stdin")
	if code > 1 {
		t.Fatalf("git check-ignore exit %d", code)
	}
	gitIgnored := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			gitIgnored[filepath.ToSlash(l)] = true
		}
	}
	if len(gitIgnored) < 25 {
		t.Fatalf("fixture too weak: git ignored only %d files: %v", len(gitIgnored), gitIgnored)
	}

	got, stats := walked(t, root)
	if stats.Skipped != 0 {
		t.Fatalf("fixture should parse cleanly; skipped %d", stats.Skipped)
	}
	yielded := map[string]bool{}
	for _, f := range got {
		yielded[f] = true
	}
	for f := range gitIgnored {
		if yielded[f] {
			t.Errorf("git ignores %s but explore would read it", f)
		}
	}
	// Not vacuous: plain files git keeps are still found.
	for _, f := range []string{"src/main.go", "plain.md", "docs/x/other.md", "foo/q/baz.txt", "deep/n.txt", "outer.txt"} {
		if gitIgnored[f] || !yielded[f] {
			t.Errorf("%s: git ignored=%v, walker yielded=%v", f, gitIgnored[f], yielded[f])
		}
	}
	var extra []string
	for _, f := range files {
		if !gitIgnored[f] && !yielded[f] {
			extra = append(extra, f)
		}
	}
	t.Logf("git ignored %d of %d; walker excluded %d more (by design): %v", len(gitIgnored), len(files), len(extra), extra)
}

func TestExploreIgnoreFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		tree    map[string]string
		want    []string
		skipped int
	}{
		{
			name: "bad nested .gitignore skips only its subtree",
			tree: map[string]string{
				"top.go":         "x\n",
				"sub/.gitignore": "ok.txt\n[unclosed\n",
				"sub/a.go":       "x\n",
				"sub/deep/b.go":  "x\n",
				"other/c.go":     "x\n",
			},
			want: []string{"other/c.go", "top.go"}, skipped: 1,
		},
		{
			name: "bad root .gitignore skips everything",
			tree: map[string]string{".gitignore": "[[:alpha:]]\n", "a.go": "x\n"},
			want: nil, skipped: 1,
		},
		{
			name: "bad .ignore counts too",
			tree: map[string]string{"sub/.ignore": "trailing\\\n", "sub/a.go": "x\n", "b.go": "x\n"},
			want: []string{"b.go"}, skipped: 1,
		},
		{
			name: "bad info/exclude skips everything",
			tree: map[string]string{".git/info/exclude": "[z-a]\n", "a.go": "x\n"},
			want: nil, skipped: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolateGitConfig(t)
			root := testutil.TempDir(t)
			writeTree(t, root, c.tree)
			got, stats := walked(t, root)
			if strings.Join(got, ",") != strings.Join(c.want, ",") || stats.Skipped != c.skipped {
				t.Fatalf("got %v skipped=%d, want %v skipped=%d", got, stats.Skipped, c.want, c.skipped)
			}
		})
	}

	t.Run("bad global excludes file skips everything", func(t *testing.T) {
		home := isolateGitConfig(t)
		writeTree(t, home, map[string]string{".config/git/ignore": "[oops\n"})
		root := testutil.TempDir(t)
		writeTree(t, root, map[string]string{"a.go": "x\n"})
		if got, stats := walked(t, root); len(got) != 0 || stats.Skipped != 1 {
			t.Fatalf("got %v skipped=%d", got, stats.Skipped)
		}
	})

	t.Run("CLI footer reports a count, never a path", func(t *testing.T) {
		isolateGitConfig(t)
		isolateHome(t)
		root := testutil.TempDir(t)
		writeTree(t, root, map[string]string{
			"a/fluxgate.go":                  "fluxgate\n",
			"prod-keys-backup/.gitignore":    "[broken\n",
			"prod-keys-backup/fluxgate.conf": "fluxgate\n",
		})
		_, text := runExplore(t, "fluxgate", "--path", root, "--no-jev")
		if !strings.Contains(text, "skipped 1 subtree") || strings.Contains(text, "prod-keys-backup") {
			t.Fatalf("footer: %s", text)
		}
		v, _ := runExplore(t, "fluxgate", "--path", root, "--no-jev", "--json")
		if len(v.Results) != 1 {
			t.Fatalf("results = %+v", v.Results)
		}
	})
}

func TestExploreIgnoreHonoursAncestorsAndGlobal(t *testing.T) {
	home := isolateGitConfig(t)
	writeTree(t, home, map[string]string{
		".gitconfig":         "[includeIf \"gitdir:~/x/\"]\n\tpath = ~/.gitconfig-client\n",
		".gitconfig-client":  "[core]\n\texcludesfile = ~/client-ignore\n",
		"client-ignore":      "*.client\n",
		".config/git/ignore": "*.xdg\n",
	})
	repo := testutil.TempDir(t)
	writeTree(t, repo, map[string]string{
		".git/HEAD":         "ref: refs/heads/main\n",
		".gitignore":        "*.fromtop\n/svc/anchored.txt\n",
		"svc/.gitignore":    "*.fromsvc\n",
		"svc/api/a.go":      "x\n",
		"svc/api/b.fromtop": "x\n",
		"svc/api/c.fromsvc": "x\n",
		"svc/api/d.client":  "x\n",
		"svc/api/e.xdg":     "x\n",
		"svc/anchored.txt":  "x\n",
	})
	all, _ := walked(t, filepath.Join(repo, "svc"))
	var got []string
	for _, f := range all {
		if filepath.Base(f) != ".gitignore" { // ignore files themselves are plain text
			got = append(got, f)
		}
	}
	if strings.Join(got, ",") != "api/a.go" {
		t.Fatalf("got %v", got)
	}
	// An unparseable ancestor .gitignore blocks a walk rooted below it.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("[bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, stats := walked(t, filepath.Join(repo, "svc")); len(got) != 0 || stats.Skipped != 1 {
		t.Fatalf("got %v skipped=%d", got, stats.Skipped)
	}
}

func TestExploreAlwaysDenyList(t *testing.T) {
	isolateGitConfig(t)
	root := testutil.TempDir(t)
	deny := []string{
		".npmrc", ".pypirc", ".netrc", ".git-credentials", ".pgpass",
		"infra/prod.tfstate", "infra/prod.tfstate.backup",
		"id_ed25519", "id_ed25519.pub", "keys/id_rsa",
		"config/my_credentials.json", "config/app-secret.yaml", "SECRETS.md",
		"dump.sql.gz", "data.dump", "app.sqlite", "app.sqlite3", "local.db",
		".aws/config", "nested/.aws/credentials", ".ssh/known_hosts", ".gnupg/pubring.kbx",
		"certs/a.p12", "a.pfx", "a.kdbx", "a.key", "a.pem",
		".env", ".env.production",
	}
	tree := map[string]string{"ok.go": "x\n"}
	for _, f := range deny {
		tree[f] = "x\n"
	}
	writeTree(t, root, tree)
	got, _ := walked(t, root)
	if strings.Join(got, ",") != "ok.go" {
		t.Fatalf("deny list let through: %v", got)
	}
}
