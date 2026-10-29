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

// Spec D §4 declares the tool as pg_prove (the framework's tool names allow
// an underscore).
func TestPgProveToolNameAccepted(t *testing.T) {
	if err := manifest.ValidatePacks(packsOf(t, strings.ReplaceAll(rlsConfig, "%TOOLS%", "[pg_prove]"))); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	withPaths := "db: {tools: [pg_prove], timeouts: {pg_prove: 300}, tool_paths: {pg_prove: /usr/local/bin/pg_prove}}"
	if err := manifest.ValidatePacks(packsOf(t, withPaths)); err != nil {
		t.Fatalf("pg_prove in timeouts and tool_paths: %v", err)
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
	// rls.tests: the repo may set it when the manifest does not, and never
	// replaces the manifest's.
	cfg = packs.Merge(packsOf(t, "db: {enabled: true}"), packsOf(t, "db: {rls: {tests: db/tests}}"))
	if got := cfg.Pack("db").String("rls.tests"); got != "db/tests" {
		t.Fatalf("repo rls.tests = %q", got)
	}
	cfg = packs.Merge(packsOf(t, "db: {enabled: true, rls: {tests: supabase/tests}}"), packsOf(t, "db: {rls: {tests: elsewhere}}"))
	if got := cfg.Pack("db").String("rls.tests"); got != "supabase/tests" {
		t.Fatalf("manifest rls.tests replaced: %q", got)
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
	// The fake pg_prove prints no harness summary: the check ran against
	// the local target and fails closed (no test is known to have run).
	if r := res["db/pgtap"]; r.Status != packs.Fail || !r.Required || !strings.Contains(r.Detail, "no harness summary") {
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
	// The tools see only what they need: no PG* variable (the connection
	// comes from test_db), the Perl module path, and a local DOCKER_HOST.
	if got := have["db/pgtap"].Env; !reflect.DeepEqual(got, []string{"PERL5LIB", "PERL_LOCAL_LIB_ROOT"}) {
		t.Errorf("db/pgtap Env = %v", got)
	}
	if got := have["db/pgtap-supabase"].Env; !reflect.DeepEqual(got, []string{"DOCKER_HOST"}) {
		t.Errorf("db/pgtap-supabase Env = %v", got)
	}
	for _, id := range []string{"db/pgtap", "db/pgtap-supabase"} {
		for _, n := range have[id].Env {
			if strings.HasPrefix(strings.ToUpper(n), "PG") {
				t.Errorf("%s passes %s from the parent environment", id, n)
			}
		}
	}
}

// pg_prove runs only .sql files (--ext .sql): a test in a .pg file never
// runs, so it never counts as coverage either.
func TestRLSCoverageCountsOnlyRunTests(t *testing.T) {
	isolate(t)
	root, changed, tracked := rlsRepo(t)
	write(t, root, "supabase/tests/database/rls_notes.pg", "select plan(1);\nselect is_empty('select 1 from public.notes', 'anon sees nothing');\n")
	in := rlsInput(t, "[]", root, append(changed, packs.ChangedFile{Path: "supabase/tests/database/rls_notes.pg", Status: 'A'}), tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}, BuiltinOnly: true})
	if rt := byID(rep)["db/rls-tests"]; rt.Status != packs.Fail || !strings.Contains(rt.Detail, "public.notes") {
		t.Fatalf("db/rls-tests with only a .pg test = %+v", rt)
	}
}

// A tests directory that pg_prove could read as an option is never used,
// whichever layer set it.
func TestRLSTestsDirNeverAnOption(t *testing.T) {
	for _, bad := range []string{"--psql-bin=evil", "-x", "db/-x"} {
		cfg := packs.Merge(packsOf(t, "db: {enabled: true}"), packsOf(t, "db: {rls: {tests: '"+bad+"'}}"))
		in := (&packs.Input{Config: cfg}).ForPack("db")
		if d := testsDir(in); d == bad || strings.HasPrefix(d, "-") || strings.Contains(d, "/-") {
			t.Errorf("%q: tests dir = %q", bad, d)
		}
	}
}
