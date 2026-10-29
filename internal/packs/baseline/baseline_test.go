package baseline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

func TestFingerprintCarriesNoValues(t *testing.T) {
	f := packs.Finding{File: `models\orders.sql`, Line: 12, Level: packs.LevelError, Rule: "LT01", Message: "column 'tester@acme.test' at position 42"}
	e := EntryFor("data/sqlfluff", "sqlfluff", f)
	raw := e.Check + e.Tool + e.Rule + e.Location // the fingerprint is a hash
	if strings.Contains(raw, "tester") || strings.Contains(raw, "42") || e.Location != "models/orders.sql" {
		t.Fatalf("entry leaks a value: %+v", e)
	}
	moved := f
	moved.Line, moved.Message = 99, "column 'tester@acme.test' at position 7"
	if Fingerprint(moved.Rule, moved.File, moved.Message) != e.Fingerprint {
		t.Fatal("digits and lines must not change the fingerprint")
	}
}

func TestSetMatchAndWrite(t *testing.T) {
	f := packs.Finding{File: "a.sql", Rule: "LT01", Message: "x"}
	s := NewSet(EntryFor("data/sqlfluff", "sqlfluff", f))
	if !s.Match("data/sqlfluff", f) || s.Match("data/other", f) || s.Match("data/sqlfluff", packs.Finding{File: "b.sql", Rule: "LT01", Message: "x"}) {
		t.Fatal("match")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, ".tq", "base.json")
	e := EntryFor("data/sqlfluff", "sqlfluff", f)
	if err := Write(p, []Entry{e, e}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || len(got.Entries) != 1 || got.Schema != Schema {
		t.Fatalf("%v %+v", err, got)
	}
	raw, _ := os.ReadFile(p)
	if Count(raw) != 1 {
		t.Fatal("count")
	}
	var m map[string]manifest.PackConfig
	yaml.Unmarshal([]byte("data: { baseline: .tq/base.json }\n"), &m)
	set, files, err := LoadForConfig(dir, packs.Merge(m, nil))
	if err != nil || set.Len() != 1 || files["data"] != ".tq/base.json" {
		t.Fatalf("%v %d %v", err, set.Len(), files)
	}
	if empty, err := Load(filepath.Join(dir, "none.json")); err != nil || len(empty.Entries) != 0 {
		t.Fatal("missing file is empty")
	}
}
