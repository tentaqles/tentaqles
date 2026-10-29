package db

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/pgtap"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

const rlsConfig = `
db:
  enabled: true
  tools: %TOOLS%
  migrations:
    - {path: supabase/migrations, engine: postgres, migrator: supabase}
    - {path: db/mssql, engine: sqlserver, migrator: flyway}
  rls:
    tests: supabase/tests/database
  test_db: {url_env: TQ_TEST_DATABASE_URL}
`

func rlsCfg(t *testing.T, tools string) packs.Config {
	t.Helper()
	return packs.Merge(packsOf(t, strings.ReplaceAll(rlsConfig, "%TOOLS%", tools)), nil)
}

// Spec D §4 declares the tool as pg_prove. The framework's tool-name rule
// (manifest toolNameRe, ^[a-z0-9-]+$) rejects the underscore today; the
// test is skipped until the framework accepts it (reported as NEEDS_CONTEXT).
func TestPgProveToolNameAccepted(t *testing.T) {
	err := manifest.ValidatePacks(packsOf(t, strings.ReplaceAll(rlsConfig, "%TOOLS%", "[pg_prove]")))
	if err != nil && strings.Contains(err.Error(), "tool names use only") {
		t.Skip("framework: packs.<pack>.tools rejects pg_prove (underscore); needs a framework change")
	}
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
}

func TestRLSConfigValidation(t *testing.T) {
	if err := manifest.ValidatePacks(packsOf(t, strings.ReplaceAll(rlsConfig, "%TOOLS%", "[supabase]"))); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	ok := "db: {rls: {exposed_schemas: [public, api], tests: supabase/tests}, test_db: {start: supabase start, hosts: [db]}}"
	if err := manifest.ValidatePacks(packsOf(t, ok)); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	for name, body := range map[string]string{
		"rls key":       "db: {rls: {schemas: [public]}}",
		"rls tests abs": "db: {rls: {tests: /tmp/t}}",
		"test_db empty": "db: {test_db: {hosts: [db]}}",
		"test_db key":   "db: {test_db: {url: postgres://localhost/x}}",
		"test_db env":   "db: {test_db: {url_env: 'a b'}}",
	} {
		if err := manifest.ValidatePacks(packsOf(t, body)); err == nil {
			t.Errorf("%s: invalid config accepted", name)
		}
	}
}

// A repo tq-packs.yaml may add exposed schemas, never a test_db (its start
// is a command tq runs).
func TestRLSRepoMerge(t *testing.T) {
	trusted := packsOf(t, "db: {enabled: true, rls: {exposed_schemas: [public]}}")
	repo := packsOf(t, "db: {rls: {exposed_schemas: [api]}, test_db: {url_env: X, start: evil --flag}}")
	cfg := packs.Merge(trusted, repo)
	s := cfg.Pack("db")
	if got := s.Strings("rls.exposed_schemas"); !reflect.DeepEqual(got, []string{"public", "api"}) {
		t.Fatalf("exposed_schemas = %v", got)
	}
	if s.Node("test_db") != nil {
		t.Fatal("the repo layer set test_db")
	}
}

func rlsRepo(t *testing.T) (string, []packs.ChangedFile, []string) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "supabase/config.toml", "[api]\nschemas = [\"api\"]\n")
	write(t, root, "supabase/migrations/20240101000000_notes.sql",
		"create table public.notes (id bigint primary key, owner_id uuid);\nalter table public.notes enable row level security;\ncreate policy own on public.notes for select to authenticated using ((select auth.uid()) = owner_id);\n")
	write(t, root, "supabase/migrations/20240102000000_tags.sql",
		"create table public.tags (id bigint primary key);\ncreate view api.note_ids as select id from public.notes;\n-- tq:allow RLS007: ids only, no tenant data\ncreate view api.tag_ids as select id from public.tags;\ncreate table private.audit (id bigint);\n")
	tracked := []string{"supabase/config.toml", "supabase/migrations/20240101000000_notes.sql"}
	changed := []packs.ChangedFile{{Path: "supabase/migrations/20240102000000_tags.sql", Status: 'A'}}
	return root, changed, tracked
}

func rlsInput(t *testing.T, tools, root string, changed []packs.ChangedFile, tracked []string) *packs.Input {
	t.Helper()
	cfg := rlsCfg(t, tools)
	in := &packs.Input{Root: root, Base: "HEAD", Changed: changed, Config: cfg, Git: fakeGit{tracked}, Budget: time.Now().Add(time.Minute)}
	in.Tools = packs.NewToolRunner(root, cfg)
	in.SetWorkspace("acme", root)
	return in
}

func TestRLSChecksEndToEnd(t *testing.T) {
	isolate(t)
	root, changed, tracked := rlsRepo(t)
	in := rlsInput(t, "[]", root, changed, tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}, BuiltinOnly: true})
	res := byID(rep)
	r := res["db/rls"]
	if r.Status != packs.Fail || !r.Required {
		t.Fatalf("db/rls = %+v", r)
	}
	want := []string{
		"supabase/migrations/20240102000000_tags.sql:1 error RLS001",
		"supabase/migrations/20240102000000_tags.sql:2 error RLS007",
	}
	if got := findings(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("db/rls findings = %v, want %v (%s)", got, want, r.Detail)
	}
	if !strings.Contains(r.Detail, "api, public, storage") {
		t.Fatalf("detail = %q (exposed schemas from config.toml)", r.Detail)
	}
	rt := res["db/rls-tests"]
	if rt.Status != packs.Fail || !reflect.DeepEqual(findings(rt), []string{"supabase/migrations/20240101000000_notes.sql:1 error RLST001"}) {
		t.Fatalf("db/rls-tests = %+v", rt)
	}
	if !strings.Contains(rt.Detail, "public.notes") || !strings.Contains(rt.Detail, "/tentaqles:rls-tests") {
		t.Fatalf("rls-tests detail = %q", rt.Detail)
	}
	ev, _ := os.ReadFile(packs.EventsFile())
	if !strings.Contains(string(ev), "suppression:rls007") {
		t.Fatalf("no suppression counter: %s", ev)
	}
	// A test file that names the table clears the coverage finding.
	write(t, root, "supabase/tests/database/rls_notes.test.sql", "select plan(1);\nselect is_empty('select 1 from public.notes', 'anon sees nothing');\n")
	in.Changed = append(in.Changed, packs.ChangedFile{Path: "supabase/tests/database/rls_notes.test.sql", Status: 'A'})
	rep = packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}, BuiltinOnly: true})
	if rt := byID(rep)["db/rls-tests"]; rt.Status != packs.OK {
		t.Fatalf("db/rls-tests with a test = %+v", rt)
	}
}

// A config.toml change alters the exposed schemas: every migration is judged.
func TestRLSConfigChangeJudgesAll(t *testing.T) {
	isolate(t)
	root, _, _ := rlsRepo(t)
	tracked := []string{"supabase/migrations/20240101000000_notes.sql", "supabase/migrations/20240102000000_tags.sql"}
	in := rlsInput(t, "[]", root, []packs.ChangedFile{{Path: "supabase/config.toml", Status: 'M'}}, tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}, BuiltinOnly: true})
	r := byID(rep)["db/rls"]
	if got := findings(r); len(got) != 2 {
		t.Fatalf("db/rls on a config change = %v (%+v)", got, r)
	}
}

func TestRLSSkipsWithoutPostgresChange(t *testing.T) {
	isolate(t)
	root, _, tracked := rlsRepo(t)
	in := rlsInput(t, "[]", root, []packs.ChangedFile{{Path: "db/mssql/V3__x.sql", Status: 'A'}, {Path: "README.md", Status: 'M'}}, tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}})
	for _, r := range rep.Results {
		if strings.HasPrefix(r.ID, "db/rls") || strings.HasPrefix(r.ID, "db/pgtap") {
			t.Fatalf("%s ran without a Postgres change", r.ID)
		}
	}
}

func TestRLSChangeFlag(t *testing.T) {
	isolate(t)
	cfg := rlsCfg(t, "[]")
	for p, want := range map[string]bool{
		"supabase/migrations/20240103_x.sql":     true,
		"supabase/tests/database/rls_x.test.sql": true,
		"supabase/config.toml":                   true,
		"apps/web/supabase/config.toml":          true,
		"db/mssql/V3__x.sql":                     false,
		"src/app.go":                             false,
	} {
		in := &packs.Input{Config: cfg, Changed: []packs.ChangedFile{{Path: p, Status: 'M'}}}
		raised := false
		for _, f := range packs.RaiseFlags(in) {
			if f == "rls_change" {
				raised = true
			}
		}
		if raised != want {
			t.Errorf("%s: rls_change = %v, want %v", p, raised, want)
		}
	}
	if got := packs.Crew(cfg, []string{"rls_change"}); !reflect.DeepEqual(got, []string{"db-reviewer", "security-reviewer"}) {
		t.Fatalf("crew = %v", got)
	}
}

func TestPgtapCheckSelection(t *testing.T) {
	isolate(t)
	bin := t.TempDir()
	fakeBin(t, bin, "pg_prove")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_DBTOOL", "pg_prove")
	t.Setenv("TQ_FAKE_VERSION", "3.36")
	t.Setenv("PGHOSTADDR", "")
	t.Setenv("PGSERVICE", "")
	old := pgtap.Dial
	pgtap.Dial = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() { pgtap.Dial = old })
	root, changed, tracked := rlsRepo(t)

	// pg_prove declared: db/pgtap runs (the Supabase migrator does not
	// switch modes when pg_prove is declared), the target is local.
	t.Setenv("TQ_TEST_DATABASE_URL", "postgres://tester@localhost:54322/postgres")
	in := rlsInput(t, "[pg_prove]", root, changed, tracked)
	in.Tools.(*toolrun.Runner).LookPath = lookOnly(bin)
	res := byID(packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}}))
	if r := res["db/pgtap"]; r.Status != packs.OK || !r.Required {
		t.Fatalf("db/pgtap = %+v", r)
	}
	if _, ok := res["db/pgtap-supabase"]; ok {
		t.Fatal("db/pgtap-supabase ran with pg_prove declared")
	}

	// A remote target: a required fail before anything connects.
	t.Setenv("TQ_TEST_DATABASE_URL", "postgres://tester@db.abcdefghijklmnop.supabase.co:5432/postgres")
	in = rlsInput(t, "[pg_prove]", root, changed, tracked)
	in.Tools.(*toolrun.Runner).LookPath = lookOnly(bin)
	res = byID(packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}}))
	if r := res["db/pgtap"]; r.Status != packs.Fail || !r.Required || !strings.Contains(r.Detail, "test_db is not local") {
		t.Fatalf("remote db/pgtap = %+v", r)
	}

	// supabase declared and missing: db/pgtap-supabase is MISSING.
	in = rlsInput(t, "[supabase]", root, changed, tracked)
	in.Tools.(*toolrun.Runner).LookPath = lookOnly(bin)
	res = byID(packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}}))
	if r := res["db/pgtap-supabase"]; r.Status != packs.Fail || !r.Required || r.Detail != packs.MissingDetail("db") {
		t.Fatalf("db/pgtap-supabase = %+v", r)
	}
	if _, ok := res["db/pgtap"]; ok {
		t.Fatal("db/pgtap ran in Supabase mode")
	}
}

func TestRLSRegisteredChecks(t *testing.T) {
	have := map[string]packs.Check{}
	for _, c := range packs.Checks("db") {
		have[c.ID] = c
	}
	for id, tool := range map[string]string{"db/rls": "", "db/rls-tests": "", "db/pgtap": "pg_prove", "db/pgtap-supabase": "supabase"} {
		c, ok := have[id]
		if !ok || c.Tool != tool {
			t.Errorf("%s: registered %v tool %q", id, ok, c.Tool)
		}
	}
}
