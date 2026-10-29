package db

// Workstream D-OI's tests (spec D §19): the packs.Count counter names the
// db pack writes, pinned against the guard, drill and check sources and the
// framework's name rule, and the db doctor probes and detection hints
// (doctor.go). The doctor tests live here because D-OI owns this test file
// and doctor.go only.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/drill"
	"github.com/tentaqles/tentaqles/internal/packs/db/pgtap"
)

// ---- counters ----------------------------------------------------------------

// migrateDeniedReasons are the reasons of db: migrate_denied:<reason> (§19
// names the counter, not the list; this pins what the guard writes).
var migrateDeniedReasons = []string{"expired", "hash-mismatch", "no-approval", "source", "unreadable", "unresolved-env", "unresolved-target", "used"}

// restoreDeniedReasons are db: restore_denied:<reason> (tq/restore-target),
// a counter beyond §19's table.
var restoreDeniedReasons = []string{"env", "linked", "non-local", "unreadable", "unresolved"}

func srcDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	return filepath.Dir(file)
}

func parseSrc(t *testing.T, rel string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(srcDir(t), filepath.FromSlash(rel)), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func strLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	return s, err == nil
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// counterLits are the string literals of f that start a counter of family.
func counterLits(f *ast.File, families string) []string {
	re := regexp.MustCompile(`^(` + families + `):[a-z0-9_.-]*$`)
	set := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			if s, ok := strLit(e); ok && re.MatchString(s) {
				set[s] = true
			}
		}
		return true
	})
	return sortedSet(set)
}

// reasonLits are the reason literals of f: the counter argument of helper
// calls (the last literal argument, or the one before a trailing
// non-literal), deniedWhy's returns, and kebab-case literals assigned to
// counter.
func reasonLits(f *ast.File, helper string) []string {
	kebab := regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
	set := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && id.Name == helper && len(x.Args) > 0 {
				i := len(x.Args) - 1
				if _, lit := strLit(x.Args[i]); !lit && i > 0 {
					i--
				}
				if s, ok := strLit(x.Args[i]); ok && kebab.MatchString(s) {
					set[s] = true
				}
			}
		case *ast.FuncDecl:
			if x.Name.Name == "deniedWhy" {
				ast.Inspect(x.Body, func(m ast.Node) bool {
					if r, ok := m.(*ast.ReturnStmt); ok && len(r.Results) == 1 {
						if s, ok := strLit(r.Results[0]); ok {
							set[s] = true
						}
					}
					return true
				})
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name == "counter" && i < len(x.Rhs) {
					if s, ok := strLit(x.Rhs[i]); ok && kebab.MatchString(s) {
						set[s] = true
					}
				}
			}
		}
		return true
	})
	return sortedSet(set)
}

func eqList(t *testing.T, what string, got, want []string) {
	t.Helper()
	g, w := append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if strings.Join(g, ",") != strings.Join(w, ",") {
		t.Errorf("%s = %q, want %q", what, g, w)
	}
}

func TestCounterNamesInTheMigrateGuard(t *testing.T) {
	f := parseSrc(t, "gate/guard.go")
	eqList(t, "migrate guard counter literals", counterLits(f, `[a-z_]+`), []string{"migrate_denied:", "migrate_denied:policy"})
	eqList(t, "migrate_denied reasons", reasonLits(f, "deny"), migrateDeniedReasons)
}

func TestCounterNamesInTheRestoreGuard(t *testing.T) {
	f := parseSrc(t, "gate/restore.go")
	eqList(t, "restore guard counter literals", counterLits(f, `[a-z_]+`), []string{"restore_denied:"})
	eqList(t, "restore_denied reasons", reasonLits(f, "deny"), restoreDeniedReasons)
}

func TestCounterNamesInTheDrill(t *testing.T) {
	eqList(t, "drill run counters", counterLits(parseSrc(t, "drill/run.go"), `[a-z_]+`), []string{"drill:fail", "drill:ok"})
	eqList(t, "drill check counters", counterLits(parseSrc(t, "drill/check.go"), `[a-z_]+`), []string{"drill_age_days:"})
}

func TestSuppressionCounterInTheDBChecks(t *testing.T) {
	for _, rel := range []string{"register_lint.go", "register_rls.go"} {
		found := false
		ast.Inspect(parseSrc(t, rel), func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || len(c.Args) != 3 {
				return true
			}
			sel, ok := c.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Count" {
				return true
			}
			pack, _ := strLit(c.Args[0])
			if b, ok := c.Args[1].(*ast.BinaryExpr); ok && pack == "db" {
				if s, _ := strLit(b.X); s == "suppression:" {
					found = true
				}
			}
			return true
		})
		if !found {
			t.Errorf("%s: no packs.Count(\"db\", \"suppression:\"+rule, n)", rel)
		}
	}
}

func readCounters(t *testing.T, pack string) map[string]int {
	t.Helper()
	out := map[string]int{}
	f, err := os.Open(packs.EventsFile())
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e struct {
			Pack    string `json:"pack"`
			Counter string `json:"counter"`
			N       int    `json:"n"`
		}
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Counter != "" && e.Pack == pack {
			out[e.Counter] += e.N
		}
	}
	return out
}

// Every db counter name survives the framework's name rule.
func TestDBCounterNamesAreRecorded(t *testing.T) {
	isolate(t)
	var want []string
	for _, r := range migrateDeniedReasons {
		want = append(want, "migrate_denied:"+r)
	}
	want = append(want, "migrate_denied:policy", "drill:ok", "drill:fail", "drill_age_days:prod-nightly", "drill_age_days:a_b",
		"suppression:pg001", "suppression:ms001", "suppression:rls001", "suppression:sup001",
	)
	for _, r := range restoreDeniedReasons {
		want = append(want, "restore_denied:"+r)
	}
	for _, c := range want {
		packs.Count("db", c, 1)
	}
	got := readCounters(t, "db")
	var names []string
	for k := range got {
		names = append(names, k)
	}
	eqList(t, "recorded", names, want)
}

const drillBlock = `
db:
  enabled: true
  drill:
    - name: prod-nightly
      engine: postgres
      source: {kind: file, path_env: TQ_DRILL_DUMP}
      freshness: 30d
`

func dbIn(t *testing.T, root, body string) *packs.Input {
	t.Helper()
	m := &manifest.Manifest{Packs: packsOf(t, body)}
	if err := manifest.ValidatePacks(m.Packs); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	in := &packs.Input{Root: root, Manifest: m, Config: packs.Merge(m.Packs, nil), Budget: time.Now().Add(time.Minute)}
	in.SetWorkspace("acme", root)
	return in.ForPack("db")
}

func evidence(t *testing.T, root string, lines ...string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(drill.EvidenceFile))
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func drillLine(name string, restored time.Time) string {
	b, _ := json.Marshal(map[string]any{"name": name, "engine": "postgres", "source": "file", "ok": true, "host_kind": "local",
		"restored_at": restored.UTC().Format(time.RFC3339), "backup_taken_at": restored.Add(-time.Hour).UTC().Format(time.RFC3339)})
	return string(b)
}

func fixClock(t *testing.T, now time.Time) {
	t.Helper()
	prev := drill.Now
	drill.Now = func() time.Time { return now }
	t.Cleanup(func() { drill.Now = prev })
}

// db/restore-drill writes the drill_age_days:<name> gauge at check time.
func TestDrillAgeGaugeWrittenByTheCheck(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	fixClock(t, now)
	evidence(t, root, drillLine("prod-nightly", now.Add(-5*24*time.Hour)))
	drill.RunCheck(dbIn(t, root, drillBlock))
	if got := readCounters(t, "db")["drill_age_days:prod-nightly"]; got != 5 {
		t.Fatalf("drill_age_days:prod-nightly = %d (%v)", got, readCounters(t, "db"))
	}
}

// ---- doctor ------------------------------------------------------------------

func codes(fs []doctor.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Level+" "+f.Code)
	}
	sort.Strings(out)
	return out
}

func find(fs []doctor.Finding, code string) (doctor.Finding, bool) {
	for _, f := range fs {
		if f.Code == code {
			return f, true
		}
	}
	return doctor.Finding{}, false
}

func noDial(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	prev := pgtap.Dial
	pgtap.Dial = func(ctx context.Context, network, addr string) error {
		calls = append(calls, network+" "+addr)
		return err
	}
	t.Cleanup(func() { pgtap.Dial = prev })
	return &calls
}

func doctorEnv(t *testing.T) string {
	t.Helper()
	isolate(t)
	for _, k := range []string{"SUPABASE_PROJECT_ID", "TQ_TEST_DATABASE_URL", "PGHOSTADDR", "PGSERVICE", "DOCKER_HOST"} {
		t.Setenv(k, "")
	}
	return t.TempDir()
}

func TestDoctorDrills(t *testing.T) {
	root := doctorEnv(t)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	fixClock(t, now)
	in := dbIn(t, root, drillBlock)
	f, ok := find(Doctor(in), "db-drill-stale")
	if !ok || f.Level != "warn" || f.Msg != "drill prod-nightly: no successful drill is recorded" || !strings.Contains(f.Fix, "tq pack db drill prod-nightly") {
		t.Fatalf("no evidence: %+v", f)
	}
	evidence(t, root, drillLine("prod-nightly", now.Add(-40*24*time.Hour)))
	if f, _ := find(Doctor(in), "db-drill-stale"); !strings.Contains(f.Msg, "40d old (freshness 30d)") {
		t.Fatalf("old drill: %+v", f)
	}
	evidence(t, root, drillLine("prod-nightly", now.Add(-40*24*time.Hour)), drillLine("prod-nightly", now.Add(-2*24*time.Hour)))
	if fs := Doctor(in); len(fs) != 0 {
		t.Fatalf("fresh drill: %q", codes(fs))
	}
	// an over-cap log cannot prove a drill
	evidence(t, root, strings.Repeat("x", drill.MaxEvidenceBytes+1))
	if f, ok := find(Doctor(in), "db-drill-log-unreadable"); !ok || f.Level != "warn" {
		t.Fatalf("over-cap log: %q", codes(Doctor(in)))
	}
	// no drill declared: nothing
	if fs := Doctor(dbIn(t, root, "db: {enabled: true}")); len(fs) != 0 {
		t.Fatalf("no drill: %q", codes(fs))
	}
}

const testDBBlock = `
db:
  enabled: true
  test_db: {url_env: TQ_TEST_DATABASE_URL, hosts: [pg]}
`

func TestDoctorTestDB(t *testing.T) {
	root := doctorEnv(t)
	in := dbIn(t, root, testDBBlock)
	calls := noDial(t, errors.New("refused"))
	if f, ok := find(Doctor(in), "db-test-db-unset"); !ok || f.Level != "warn" || !strings.Contains(f.Msg, "TQ_TEST_DATABASE_URL is not set") {
		t.Fatalf("unset: %+v", f)
	}
	for _, c := range []struct{ url, want, never string }{
		{"postgres://tester:hunter2@db.acme.test:5432/app", "error db-test-db-not-local", "hunter2"},       // tq:allow-secret (fake test fixture)
		{"host=10.0.0.5 user=tester password=hunter2 dbname=app", "error db-test-db-not-local", "hunter2"}, // tq:allow-secret (fake test fixture)
		{"mysql://tester:hunter2@localhost/app", "error db-test-db-invalid", "hunter2"},                    // tq:allow-secret (fake test fixture)
		{"postgres://tester:hunter2@localhost:5999/app", "warn db-test-db-down", "hunter2"},                // tq:allow-secret (fake test fixture)
		{"postgres://tester:hunter2@pg:5432/app", "", "hunter2"},                                           // a compose service, not dialled; tq:allow-secret (fake test fixture)
	} {
		*calls = nil
		t.Setenv("TQ_TEST_DATABASE_URL", c.url)
		fs := Doctor(in)
		if got := strings.Join(codes(fs), ","); got != c.want {
			t.Errorf("%s: %q, want %q", c.url, got, c.want)
		}
		for _, f := range fs {
			if strings.Contains(f.Msg+f.Fix, c.never) {
				t.Errorf("%s: the connection secret reached the finding: %+v", c.url, f)
			}
		}
		switch {
		case strings.Contains(c.url, "@localhost:5999"):
			if len(*calls) != 1 || (*calls)[0] != "tcp localhost:5999" {
				t.Errorf("probe calls %q", *calls)
			}
		case len(*calls) != 0:
			t.Errorf("%s: dialled %q", c.url, *calls)
		}
	}
	noDial(t, nil)
	t.Setenv("TQ_TEST_DATABASE_URL", "postgres://tester@127.0.0.1:5432/app")
	if fs := Doctor(in); len(fs) != 0 {
		t.Fatalf("answering local db: %q", codes(fs))
	}
}

func TestDoctorTestDBUnsetForPgProveAndSupabase(t *testing.T) {
	root := doctorEnv(t)
	calls := noDial(t, errors.New("refused"))
	if f, ok := find(Doctor(dbIn(t, root, "db: {enabled: true, tools: [pg_prove]}")), "db-test-db-unset"); !ok || !strings.Contains(f.Msg, "pg_prove is declared") {
		t.Fatalf("pg_prove without test_db: %+v", f)
	}
	f, ok := find(Doctor(dbIn(t, root, "db: {enabled: true, tools: [supabase]}")), "db-test-db-down")
	if !ok || f.Level != "info" || !strings.Contains(f.Msg, "127.0.0.1:54322") {
		t.Fatalf("supabase default port: %+v", f)
	}
	os.MkdirAll(filepath.Join(root, "supabase"), 0o755)
	os.WriteFile(filepath.Join(root, "supabase", "config.toml"), []byte("[db]\nport = 55432\n"), 0o644)
	*calls = nil
	Doctor(dbIn(t, root, "db: {enabled: true, tools: [supabase]}"))
	if len(*calls) != 1 || (*calls)[0] != "tcp 127.0.0.1:55432" {
		t.Fatalf("config.toml port: %q", *calls)
	}
}

const refBlock = `
db:
  enabled: true
  environments:
    prod:
      match: {project_refs: [abcdefghijklmnopqrst]}
`

func TestDoctorSupabaseLinkedRef(t *testing.T) {
	root := doctorEnv(t)
	noDial(t, nil)
	in := dbIn(t, root, refBlock)
	if fs := Doctor(in); len(fs) != 0 {
		t.Fatalf("no linked project: %q", codes(fs))
	}
	os.MkdirAll(filepath.Join(root, "supabase", ".temp"), 0o755)
	os.WriteFile(filepath.Join(root, "supabase", ".temp", "project-ref"), []byte("zyxwvutsrqponmlkjihg\n"), 0o644)
	f, ok := find(Doctor(in), "db-supabase-ref-undeclared")
	if !ok || f.Level != "warn" || strings.Contains(f.Msg, "zyxwvutsrqponmlkjihg") || !strings.Contains(f.Msg, "not listed in any environment") {
		t.Fatalf("undeclared ref: %+v", f)
	}
	t.Setenv("SUPABASE_PROJECT_ID", "abcdefghijklmnopqrst") // the CLI's order: the variable first
	if _, ok := find(Doctor(in), "db-supabase-ref-undeclared"); ok {
		t.Fatal("a declared ref was flagged")
	}
	t.Setenv("SUPABASE_PROJECT_ID", "")
	if f, _ := find(Doctor(dbIn(t, root, "db: {enabled: true}")), "db-supabase-ref-undeclared"); !strings.Contains(f.Msg, "declares no environments") {
		t.Fatalf("no environments: %+v", f)
	}
}

func TestDoctorDBEnvironments(t *testing.T) {
	root := doctorEnv(t)
	noDial(t, nil)
	in := dbIn(t, root, `
db:
  enabled: true
  environments:
    staging: {match: {hosts: [db.acme.test]}}
    prod: {match: {hosts: [DB.acme.test], servers: [sql-prod]}}
`)
	fs := Doctor(in)
	f, ok := find(fs, "db-env-overlap")
	if !ok || !strings.Contains(f.Msg, "prod and staging both match hosts") || !strings.Contains(f.Msg, "(prod)") || len(fs) != 1 {
		t.Fatalf("overlap: %q %+v", codes(fs), f)
	}
}

func TestDBHintsFollowMigrationDirRE(t *testing.T) {
	if got := packs.Hints("db"); len(got) != len(Hints) {
		t.Fatalf("hints registered: %d of %d", len(got), len(Hints))
	}
	isolate(t)
	root := t.TempDir()
	in := &packs.Input{Root: root, Config: packs.Merge(nil, nil)}
	for _, p := range []string{
		"migrations/0001_init.sql", "app/migrations/0002_users.py", "supabase/migrations/20260101_x.sql",
		"prisma/migrations/20260101/migration.sql", "db/migrate/20260101_create.rb", "cmd/migrate/main.go",
		"alembic/versions/abc_rev.py", "flyway/V1__init.sql", "sql/migrations/V2__x.sql", "a/b/migrations/deep/x.sql",
		"migrations", "migrate.go", "docs/migrations.md", "alembic/env.py", "src/app.py", "db/schema.sql",
	} {
		want := MigrationDirRE.MatchString(p)
		var got []string
		for _, s := range packs.Suggest(in, []string{p}) {
			if strings.HasPrefix(s, "db pack:") {
				got = append(got, s)
			}
		}
		if want != (len(got) == 1) {
			t.Errorf("%s: MIGRATION_DIR_RE %v, hints %q", p, want, got)
		}
		if want && len(got) == 1 && got[0] != "db pack: database migrations ("+p+")" {
			t.Errorf("%s: %q", p, got[0])
		}
	}
}
