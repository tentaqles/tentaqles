package data

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
)

// TestTemplateDBTContractStarter runs every built-in check registered in
// this package over templates/dbt-contract-starter as `tq pack check --all
// --builtin-only` would, with the template's own tq-packs.yaml: no finding
// of any level is allowed (spec C §1.4, §5.5).
func TestTemplateDBTContractStarter(t *testing.T) {
	isolate(t)
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "templates", "dbt-contract-starter"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, manifest.RepoPacksFile))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := manifest.ParseRepoPacks(raw)
	if err != nil {
		t.Fatalf("the template's tq-packs.yaml is invalid: %v", err)
	}
	var changed []packs.ChangedFile
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "__pycache__" || n == ".pytest_cache" || n == "target" || n == "logs" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		changed = append(changed, packs.ChangedFile{Path: filepath.ToSlash(rel), Status: 'A'})
		return nil
	})
	cfg := packs.Merge(packsOf(t, "data: {enabled: true}"), repo)
	in := &packs.Input{Root: root, All: true, Changed: changed, Config: cfg, Budget: time.Now().Add(time.Minute)}
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"data"}, BuiltinOnly: true})
	ran := map[string]bool{}
	for _, r := range rep.Results {
		ran[r.ID] = true
		t.Logf("%s: %s %s", r.ID, r.Status, r.Detail)
		if r.Status == packs.Fail || len(r.Findings) > 0 {
			t.Errorf("%s: %s %s %+v", r.ID, r.Status, r.Detail, r.Findings)
		}
	}
	for _, id := range []string{idNewModelTests, idContractEnforced, idIncremental, idSelectStar, idHardcodedRel} {
		if !ran[id] {
			t.Errorf("%s did not run over the template", id)
		}
	}
}
