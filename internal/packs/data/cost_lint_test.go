package data

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

var updateCostGolden = flag.Bool("update-cost", false, "rewrite the cost golden files")

func costCheck(t *testing.T, id string) packs.Check {
	t.Helper()
	for _, c := range packs.Checks("data") {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("check %s not registered", id)
	return packs.Check{}
}

// costLintInput is an Input over a fixture repo with every file changed.
func costLintInput(t *testing.T, root string, cfg packs.Config) *packs.Input {
	t.Helper()
	in := &packs.Input{Root: root, Config: cfg}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			in.Changed = append(in.Changed, packs.ChangedFile{Path: filepath.ToSlash(rel), Status: 'A'})
		}
		return nil
	})
	sort.Slice(in.Changed, func(i, j int) bool { return in.Changed[i].Path < in.Changed[j].Path })
	return in.ForPack("data")
}

func costGolden(t *testing.T, path string, got any) {
	t.Helper()
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if *updateCostGolden {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s: %v (run with -update-cost)", path, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), b) {
		t.Fatalf("%s differs:\n got: %s\nwant: %s", path, b, want)
	}
}

func TestBQJobMaxBytesGolden(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	c := costCheck(t, "data/bq-job-max-bytes")
	in := costLintInput(t, filepath.Join("testdata", "cost-lint", "repo"), cfg)
	if !c.Applies(in) {
		t.Fatal("does not apply")
	}
	r := c.Run(context.Background(), in)
	costGolden(t, filepath.Join("testdata", "cost-lint", "want.json"), r)
	for _, f := range r.Findings {
		if strings.Contains(f.Message, "acme.orders") || strings.Contains(f.Message, "SELECT") {
			t.Fatalf("a finding carries query text: %+v", f)
		}
	}
}

func TestBQJobMaxBytesNeedsEngine(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, "packs:\n  data:\n    engines: [snowflake]\n")
	c := costCheck(t, "data/bq-job-max-bytes")
	if c.Applies(costLintInput(t, filepath.Join("testdata", "cost-lint", "repo"), cfg)) {
		t.Fatal("applies without bigquery in engines")
	}
	if c.Kind != packs.Builtin {
		t.Fatal("must be built-in")
	}
}
