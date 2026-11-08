package integration

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mdLink finds the target of every inline Markdown link: [text](target).
var mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// docFiles are the Markdown files a reader follows: the README, CLAUDE.md and everything under docs/.
func docFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	files := []string{filepath.Join(root, "README.md"), filepath.Join(root, "CLAUDE.md")}
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// Every relative link in the docs points at a file that exists.
func TestDocsRelativeLinksResolve(t *testing.T) {
	for _, f := range docFiles(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range mdLink.FindAllStringSubmatch(string(raw), -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			if _, err := os.Stat(filepath.Join(filepath.Dir(f), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s links to %s, which does not exist", f, m[1])
			}
		}
	}
}

// docs/README.md is the index: every page under docs/ is listed in it.
func TestDocsIndexListsEveryPage(t *testing.T) {
	docs := filepath.Join("..", "..", "docs")
	index, err := os.ReadFile(filepath.Join(docs, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range docFiles(t) {
		rel, err := filepath.Rel(docs, f)
		if err != nil || strings.HasPrefix(rel, "..") || rel == "README.md" {
			continue
		}
		if !strings.Contains(string(index), "("+filepath.ToSlash(rel)+")") {
			t.Errorf("docs/README.md does not list %s", filepath.ToSlash(rel))
		}
	}
}
