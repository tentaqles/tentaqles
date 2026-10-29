package data

// Golden harness for the contract checks: testdata/<check>/<case>/{old,new}/
// hold the two sides of a change, config.yaml the optional packs.data keys
// (and a manifest data: block under "manifest_data:"), want.json the result.
// TQ_CONTRACT_UPDATE=1 rewrites want.json.

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

type ctFakeGit struct {
	old, new string
	files    []string
}

func (g ctFakeGit) Show(ref, p string) ([]byte, error) {
	if ref == ":" {
		return os.ReadFile(filepath.Join(g.new, filepath.FromSlash(p)))
	}
	if g.old == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(g.old, filepath.FromSlash(p)))
}

func (g ctFakeGit) ListFiles() ([]string, error) { return g.files, nil }

// ctFiles lists the files under dir (slash paths), skipping .keep markers.
func ctFiles(t testing.TB, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	if _, err := os.Stat(dir); err != nil {
		return out
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == ".keep" {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		out[filepath.ToSlash(rel)] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type ctCaseConfig struct {
	Data     yaml.Node     `yaml:"data"`
	Manifest manifest.Data `yaml:"manifest_data"`
}

// ctInput builds the data pack's Input for a case directory: the new side
// is the working tree, the old side is Base "HEAD" (absent when the case has
// no old/ directory), and every file that differs is changed.
func ctInput(t testing.TB, dir string) *packs.Input {
	t.Helper()
	ctIsolate(t)
	oldDir, newDir := filepath.Join(dir, "old"), filepath.Join(dir, "new")
	olds, news := ctFiles(t, oldDir), ctFiles(t, newDir)
	var changed []packs.ChangedFile
	var tracked []string
	for p, b := range news {
		tracked = append(tracked, p)
		ob, had := olds[p]
		switch {
		case !had:
			changed = append(changed, packs.ChangedFile{Path: p, Status: 'A'})
		case !bytes.Equal(ob, b):
			changed = append(changed, packs.ChangedFile{Path: p, Status: 'M'})
		}
	}
	for p := range olds {
		if _, ok := news[p]; !ok {
			changed = append(changed, packs.ChangedFile{Path: p, Status: 'D'})
		}
	}
	sort.Strings(tracked)
	sort.Slice(changed, func(i, j int) bool { return changed[i].Path < changed[j].Path })
	var cc ctCaseConfig
	if b, err := os.ReadFile(filepath.Join(dir, "config.yaml")); err == nil {
		if err := yaml.Unmarshal(b, &cc); err != nil {
			t.Fatalf("%s/config.yaml: %v", dir, err)
		}
	}
	m := &manifest.Manifest{Data: cc.Manifest}
	base := "HEAD"
	if _, err := os.Stat(oldDir); err != nil {
		base = ""
		oldDir = ""
	}
	in := &packs.Input{Root: newDir, Base: base, Changed: changed, Manifest: m,
		Config: ctConfig(t, cc.Data), Git: ctFakeGit{old: oldDir, new: newDir, files: tracked}}
	in.SetWorkspace("acme", dir)
	return in.ForPack("data")
}

// ctConfig is a merged config with the data pack enabled and keys from n.
func ctConfig(t testing.TB, n yaml.Node) packs.Config {
	t.Helper()
	body := "data:\n  enabled: true\n"
	if n.Kind == yaml.MappingNode {
		b, err := yaml.Marshal(&n)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			body += "  " + l + "\n"
		}
	}
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	return packs.Merge(m, nil)
}

func ctIsolate(t testing.TB) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

type ctWant struct {
	Status   packs.Status    `json:"status"`
	Detail   string          `json:"detail"`
	Findings []packs.Finding `json:"findings"`
}

// ctGolden runs run over every case of testdata/<check>/ and compares the
// result with want.json.
func ctGolden(t *testing.T, check string, run func(context.Context, *packs.Input) packs.Result) {
	t.Helper()
	root := filepath.Join("testdata", check)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatalf("no cases under %s", root)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			in := ctInput(t, dir)
			r := run(context.Background(), in)
			got := ctWant{Status: r.Status, Detail: r.Detail, Findings: r.Findings}
			if got.Findings == nil {
				got.Findings = []packs.Finding{}
			}
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetIndent("", "  ")
			enc.SetEscapeHTML(false)
			if err := enc.Encode(got); err != nil {
				t.Fatal(err)
			}
			gb := buf.Bytes()
			wantPath := filepath.Join(dir, "want.json")
			if os.Getenv("TQ_CONTRACT_UPDATE") == "1" {
				if err := os.WriteFile(wantPath, gb, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			wb, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("missing %s (run with TQ_CONTRACT_UPDATE=1 and review it): %v", wantPath, err)
			}
			if !bytes.Equal(bytes.ReplaceAll(wb, []byte("\r\n"), []byte("\n")), gb) {
				t.Errorf("%s mismatch\n--- got\n%s\n--- want\n%s", e.Name(), gb, wb)
			}
		})
	}
}
