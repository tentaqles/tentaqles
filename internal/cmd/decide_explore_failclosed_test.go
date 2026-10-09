package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

func mkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// chmodOrSkip makes p unreadable, or skips where permissions do not bite
// (Windows, or running as root).
func chmodOrSkip(t *testing.T, p string, isDir bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not remove read access on Windows")
	}
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0o644)
	if isDir {
		mode = 0o755
	}
	t.Cleanup(func() { os.Chmod(p, mode) })
	var err error
	if isDir {
		_, err = os.ReadDir(p)
	} else {
		_, err = os.ReadFile(p)
	}
	if err == nil {
		t.Skip("running with privileges that bypass chmod")
	}
}

// TestExploreSourcePresentButUnusableFailsClosed covers every way an ignore
// source can exist yet not be applied: each must skip what it governs.
func TestExploreSourcePresentButUnusableFailsClosed(t *testing.T) {
	base := map[string]string{"top.go": "x\n", "sub/a.go": "x\n", "sub/deep/b.go": "x\n"}
	cases := []struct {
		name    string
		setup   func(t *testing.T, home, root string)
		want    string // walked files, comma-joined
		skipped int
	}{
		{
			name:  "nested .gitignore is a directory",
			setup: func(t *testing.T, _, root string) { mkdirAll(t, filepath.Join(root, "sub", ".gitignore")) },
			want:  "top.go", skipped: 1,
		},
		{
			name:  "nested .rgignore is a directory",
			setup: func(t *testing.T, _, root string) { mkdirAll(t, filepath.Join(root, "sub", ".rgignore")) },
			want:  "top.go", skipped: 1,
		},
		{
			name: "nested .gitignore unreadable (unix)",
			setup: func(t *testing.T, _, root string) {
				p := filepath.Join(root, "sub", ".gitignore")
				writeTree(t, root, map[string]string{"sub/.gitignore": "*.tmp\n"})
				chmodOrSkip(t, p, false)
			},
			want: "top.go", skipped: 1,
		},
		{
			name: "directory unreadable (unix)",
			setup: func(t *testing.T, _, root string) {
				chmodOrSkip(t, filepath.Join(root, "sub"), true)
			},
			want: "top.go", skipped: 1,
		},
		{
			name: "nested .gitignore not UTF-8",
			setup: func(t *testing.T, _, root string) {
				writeTree(t, root, map[string]string{"sub/.gitignore": "\xff\xfe*.log\n"})
			},
			want: "top.go", skipped: 1,
		},
		{
			name: "nested .gitignore too large",
			setup: func(t *testing.T, _, root string) {
				writeTree(t, root, map[string]string{"sub/.gitignore": strings.Repeat("a\n", maxExploreFile)})
			},
			want: "top.go", skipped: 1,
		},
		{
			name:  "root .gitignore is a directory",
			setup: func(t *testing.T, _, root string) { mkdirAll(t, filepath.Join(root, ".gitignore")) },
			want:  "", skipped: 1,
		},
		{
			name:  "info/exclude is a directory",
			setup: func(t *testing.T, _, root string) { mkdirAll(t, filepath.Join(root, ".git", "info", "exclude")) },
			want:  "", skipped: 1,
		},
		{
			name:  "global excludes file is a directory",
			setup: func(t *testing.T, home, _ string) { mkdirAll(t, filepath.Join(home, ".config", "git", "ignore")) },
			want:  "", skipped: 1,
		},
		{
			name: "configured excludesFile is a directory",
			setup: func(t *testing.T, home, _ string) {
				writeTree(t, home, map[string]string{".gitconfig": "[core]\n\texcludesfile = ~/exdir\n"})
				mkdirAll(t, filepath.Join(home, "exdir"))
			},
			want: "", skipped: 1,
		},
		{
			name:  "global config is a directory",
			setup: func(t *testing.T, home, _ string) { mkdirAll(t, filepath.Join(home, ".gitconfig")) },
			want:  "", skipped: 1,
		},
		{
			name: "bad include",
			setup: func(t *testing.T, home, _ string) {
				writeTree(t, home, map[string]string{".gitconfig": "[include]\n\tpath = ~someoneelse/.gitconfig\n"})
			},
			want: "", skipped: 1,
		},
		{
			name: "unresolvable excludesFile",
			setup: func(t *testing.T, home, _ string) {
				writeTree(t, home, map[string]string{".gitconfig": "[core]\n\texcludesfile = %(prefix)/etc/gitignore\n"})
			},
			want: "", skipped: 1,
		},
		{
			name: "repo config unparsable",
			setup: func(t *testing.T, _, root string) {
				writeTree(t, root, map[string]string{".git/config": "[core\n\texcludesfile = x\n"})
			},
			want: "", skipped: 1,
		},
		{
			name: "broken gitdir pointer",
			setup: func(t *testing.T, _, root string) {
				writeTree(t, root, map[string]string{".git": "gitdir: ../does-not-exist/.git/worktrees/x\n"})
			},
			want: "", skipped: 1,
		},
		{
			name: "broken commondir",
			setup: func(t *testing.T, _, root string) {
				writeTree(t, root, map[string]string{
					".git":          "gitdir: .gd\n",
					".gd/commondir": "../nowhere\n",
				})
			},
			want: "", skipped: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := isolateGitConfig(t)
			root := testutil.TempDir(t)
			writeTree(t, root, base)
			c.setup(t, home, root)
			got, stats := walked(t, root)
			var files []string
			for _, f := range got {
				if !strings.HasPrefix(f, ".gd/") { // the fake gitdir is plain text here
					files = append(files, f)
				}
			}
			if strings.Join(files, ",") != c.want || stats.Skipped != c.skipped {
				t.Fatalf("got %v skipped=%d, want %q skipped=%d", files, stats.Skipped, c.want, c.skipped)
			}
		})
	}
}

func TestExploreAncestorSourceUnusableBlocksWalk(t *testing.T) {
	isolateGitConfig(t)
	repo := testutil.TempDir(t)
	writeTree(t, repo, map[string]string{".git/HEAD": "ref: refs/heads/main\n", "svc/a.go": "x\n"})
	mkdirAll(t, filepath.Join(repo, ".gitignore"))
	if got, stats := walked(t, filepath.Join(repo, "svc")); len(got) != 0 || stats.Skipped != 1 {
		t.Fatalf("got %v skipped=%d", got, stats.Skipped)
	}
}

// TestExploreSkipsNestedRepos: a directory below the root holding its own
// .git (a nested clone, submodule or worktree) is never entered.
func TestExploreSkipsNestedRepos(t *testing.T) {
	isolateGitConfig(t)
	isolateHome(t)
	root := testutil.TempDir(t)
	writeTree(t, root, map[string]string{
		".git/HEAD":                        "ref: refs/heads/main\n",
		"own/fluxgate.go":                  "fluxgate\n",
		"clients/acme/.git/HEAD":           "ref: refs/heads/main\n",
		"clients/acme/fluxgate.go":         "fluxgate acme\n",
		"clients/acme/deep/fluxgate.go":    "fluxgate acme\n",
		"vendor-ish/lib/.git":              "gitdir: ../../.git/modules/lib\n",
		"vendor-ish/lib/fluxgate.go":       "fluxgate lib\n",
		".git/modules/lib/HEAD":            "ref: refs/heads/main\n",
		"clients/plain-folder/fluxgate.go": "fluxgate plain\n",
	})
	got, stats := walked(t, root)
	if strings.Join(got, ",") != "clients/plain-folder/fluxgate.go,own/fluxgate.go" || stats.NestedRepos != 2 {
		t.Fatalf("got %v nested=%d", got, stats.NestedRepos)
	}
	_, text := runExplore(t, "fluxgate", "--path", root, "--no-jev")
	if !strings.Contains(text, "skipped 2 nested repo(s)") || strings.Contains(text, "acme") {
		t.Fatalf("footer: %s", text)
	}
	// --path inside the nested repo searches it: its own repo is the root's.
	got, stats = walked(t, filepath.Join(root, "clients", "acme"))
	if strings.Join(got, ",") != "deep/fluxgate.go,fluxgate.go" || stats.NestedRepos != 0 {
		t.Fatalf("inside nested repo: got %v nested=%d", got, stats.NestedRepos)
	}
}
