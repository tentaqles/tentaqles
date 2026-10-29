package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

type fakeRunner struct {
	out   toolrun.Output
	err   error
	specs []toolrun.Spec
}

func (f *fakeRunner) Probe(pack, tool string) toolrun.Probe {
	return toolrun.Probe{Tool: tool, Found: true, Path: "/fake/" + tool, Version: "2.70.0"}
}

func (f *fakeRunner) Run(ctx context.Context, s toolrun.Spec) (toolrun.Output, error) {
	f.specs = append(f.specs, s)
	return f.out, f.err
}

func golden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func strs(fs []packs.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}

func TestParseSquawk(t *testing.T) {
	root := filepath.FromSlash("/repo")
	fs, err := ParseSquawk(golden(t, "squawk.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	got := strs(fs)
	want := []string{
		"supabase/migrations/20240101000000_orders.sql:3 error squawk:adding-required-field Adding a field that is `NOT NULL` without a default will fail on a populated table. Make the field nullable or add a non-VOLATILE DEFAULT.",
		"supabase/migrations/20240102000000_idx.sql:1 error squawk:require-concurrent-index-creation During normal index creation, table updates are blocked, but reads are still allowed. Use `concurrently` to avoid blocking writes.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if _, err := ParseSquawk([]byte("not json"), root); err == nil {
		t.Fatal("bad JSON must error")
	}
}

func TestParseSqlfluff(t *testing.T) {
	fs, err := ParseSqlfluff(golden(t, "sqlfluff.json"), "/repo", false)
	if err != nil {
		t.Fatal(err)
	}
	got := strs(fs)
	want := []string{
		"db/mssql/V2__orders.sql:3 warn sqlfluff:CP01 Keywords must be consistently upper case.",
		"db/mssql/V2__orders.sql:5 warn sqlfluff:LT01 Expected single whitespace, contact [REDACTED:email].",
		"db/mssql/V4__old.sql:7 warn sqlfluff:L010 Keywords must be consistently upper case.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	fs, _ = ParseSqlfluff(golden(t, "sqlfluff.json"), "/repo", true)
	if fs[0].Level != packs.LevelError || fs[1].Level != packs.LevelWarn {
		t.Fatalf("sqlfluff_fail: CP01 fails, a sqlfluff warning stays a warning: %v", strs(fs))
	}
}

func TestSquawkRun(t *testing.T) {
	in := &packs.Input{Root: "/repo"}
	f := &fakeRunner{out: toolrun.Output{Stdout: golden(t, "squawk.json"), ExitCode: 1, Version: "2.70.0"}}
	in.Tools = f
	r := Squawk(context.Background(), in, []string{"supabase/migrations/20240101000000_orders.sql", "supabase/migrations/20240102000000_idx.sql"})
	if r.Status != packs.Fail || len(r.Findings) != 2 || r.Tool == nil || r.Tool.ExitCode != 1 {
		t.Fatalf("result = %+v", r)
	}
	if got := f.specs[0].Args; !reflect.DeepEqual(got[:2], []string{"--reporter", "json"}) || len(got) != 4 || f.specs[0].Pack != "db" || f.specs[0].Tool != "squawk" {
		t.Fatalf("argv = %v", got)
	}
	f.out = toolrun.Output{Stdout: []byte("[]\n"), ExitCode: 0}
	if r := Squawk(context.Background(), in, []string{"a.sql"}); r.Status != packs.OK || len(r.Findings) != 0 {
		t.Fatalf("clean = %+v", r)
	}
	f.out = toolrun.Output{Stderr: []byte("boom for tester@acme.test\n"), ExitCode: 2}
	r = Squawk(context.Background(), in, []string{"a.sql"})
	if r.Status != packs.Fail || strings.Contains(r.Detail, "tester@acme.test") {
		t.Fatalf("crash = %+v", r)
	}
	f.out = toolrun.Output{TimedOut: true, ExitCode: -1}
	if r := Squawk(context.Background(), in, []string{"a.sql"}); r.Status != packs.Fail || !strings.Contains(r.Detail, "timed out") {
		t.Fatalf("timeout = %+v", r)
	}
	f.err = errors.New("no such tool")
	if r := Squawk(context.Background(), in, []string{"a.sql"}); r.Status != packs.Fail {
		t.Fatalf("run error = %+v", r)
	}
}

func TestSqlfluffRun(t *testing.T) {
	in := &packs.Input{Root: "/repo"}
	f := &fakeRunner{out: toolrun.Output{Stdout: golden(t, "sqlfluff.json"), ExitCode: 1}}
	in.Tools = f
	r := Sqlfluff(context.Background(), in, "tsql", []string{"db/mssql/V2__orders.sql"}, false)
	if r.Status != packs.OK || len(r.Findings) != 3 {
		t.Fatalf("style warnings never fail: %+v", r)
	}
	want := []string{"lint", "--format", "json", "--dialect", "tsql", "db/mssql/V2__orders.sql"}
	if !reflect.DeepEqual(f.specs[0].Args, want) {
		t.Fatalf("argv = %v", f.specs[0].Args)
	}
	if r := Sqlfluff(context.Background(), in, "tsql", []string{"x.sql"}, true); r.Status != packs.Fail {
		t.Fatalf("sqlfluff_fail: %+v", r)
	}
	f.out = toolrun.Output{Stdout: []byte("garbage"), ExitCode: 2}
	if r := Sqlfluff(context.Background(), in, "postgres", []string{"x.sql"}, false); r.Status != packs.Fail {
		t.Fatalf("usage error: %+v", r)
	}
}

func TestBatchesLongFileLists(t *testing.T) {
	in := &packs.Input{Root: "/repo"}
	f := &fakeRunner{out: toolrun.Output{Stdout: []byte("[]"), ExitCode: 0}}
	in.Tools = f
	var many []string
	for i := 0; i < 250; i++ {
		many = append(many, "m/"+strings.Repeat("x", 10)+".sql")
	}
	Squawk(context.Background(), in, many)
	if len(f.specs) != 3 {
		t.Fatalf("runs = %d", len(f.specs))
	}
}
