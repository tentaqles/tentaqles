package integration

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles"
)

// thirdPartyFiles are the license texts of the projects tentaqles draws on, one per project.
var thirdPartyFiles = []string{
	"third_party/licenses/trail-code/LICENSE",
	"third_party/licenses/agentmemory/LICENSE",
	"third_party/licenses/graphify/LICENSE",
	"third_party/licenses/graphify/NOTICE",
	"third_party/licenses/graft/LICENSE",
}

// portedHeader is the comment a ported Go file opens with, before its package line.
var portedHeader = regexp.MustCompile(`^// Ported from (Trail-code|agentmemory|Graphify|Graft) \S+ at [0-9a-f]{40} \((MIT|Apache-2\.0)\)\.$`)

func TestRootLicenseIsMIT(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"MIT License", "Permission is hereby granted, free of charge"} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("LICENSE does not contain %q", want)
		}
	}
}

func TestNoticeListsEveryPort(t *testing.T) {
	root := filepath.Join("..", "..")
	notice, err := os.ReadFile(filepath.Join(root, "NOTICE"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(notice)
	for _, name := range []string{"Trail-code", "agentmemory", "Graphify", "Graft"} {
		if !strings.Contains(text, name) {
			t.Fatalf("NOTICE does not name %s", name)
		}
	}
	if strings.Contains(text, "BASE") {
		t.Fatal("NOTICE must not name BASE")
	}

	for _, f := range thirdPartyFiles {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", f)
		}
	}

	// Every ported Go file under internal/ and at the root names a project NOTICE lists.
	for _, dir := range []string{filepath.Join(root, "internal"), root} {
		walk := func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				if d.IsDir() && p != dir && dir == root {
					return fs.SkipDir
				}
				return nil
			}
			return checkPortedHeader(t, p, text)
		}
		if err := filepath.WalkDir(dir, walk); err != nil {
			t.Fatal(err)
		}
	}

	// No file under internal/lobe names the BASE project.
	err = filepath.WalkDir(filepath.Join(root, "internal", "lobe"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "Ported from BASE") {
			t.Fatalf("%s names BASE", p)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// checkPortedHeader checks the `// Ported from` lines before a Go file's package line.
func checkPortedHeader(t *testing.T, path, notice string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "package ") {
			break
		}
		if !strings.HasPrefix(line, "// Ported from ") {
			continue
		}
		m := portedHeader.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("%s: bad ported header %q", path, line)
			continue
		}
		if !strings.Contains(notice, m[1]) {
			t.Errorf("%s: NOTICE does not name %s", path, m[1])
		}
	}
	return nil
}

func TestNoticesAreEmbedded(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, f := range []string{"LICENSE", "NOTICE", "third_party/licenses/trail-code/LICENSE"} {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := tentaqles.Notices.ReadFile(f)
		if err != nil {
			t.Fatalf("embedded %s: %v", f, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("embedded %s differs from the file on disk", f)
		}
	}
}
