package db

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/db/lint"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"gopkg.in/yaml.v3"
)

// TestMain doubles as the fake squawk and sqlfluff: a copy of the test
// binary run with TQ_FAKE_DBTOOL set prints a canned output and records its
// argv.
func TestMain(m *testing.M) {
	if mode := os.Getenv("TQ_FAKE_DBTOOL"); mode != "" {
		fakeTool(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeTool(mode string) {
	args := os.Args[1:]
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-V") {
		fmt.Println(mode + " " + os.Getenv("TQ_FAKE_VERSION"))
		os.Exit(0)
	}
	if f := os.Getenv("TQ_FAKE_ARGV"); f != "" {
		_ = os.WriteFile(f, []byte(strings.Join(args, "\n")), 0o600)
	}
	if f := os.Getenv("TQ_FAKE_SEEN"); f != "" && len(args) > 0 {
		// Record what the tool sees at its last file argument, from its cwd.
		b, _ := os.ReadFile(args[len(args)-1])
		_ = os.WriteFile(f, b, 0o600)
	}
	if mode == "sqlfluff-tsql-broken" {
		for _, a := range args {
			if a == "tsql" {
				fmt.Fprintln(os.Stderr, "User Error: bad config")
				os.Exit(2)
			}
		}
		mode = "sqlfluff"
	}
	switch mode {
	case "squawk":
		fmt.Print(`[{"file":"supabase/migrations/20240102000000_idx.sql","line":1,"column":0,"level":"Warning","message":"During normal index creation, table updates are blocked.","note":null,"help":"Use concurrently.","rule_name":"require-concurrent-index-creation","column_end":9,"line_end":1}]`)
		os.Exit(1)
	case "sqlfluff":
		fmt.Print(`[{"filepath":"supabase/migrations/20240102000000_idx.sql","violations":[{"start_line_no":2,"start_line_pos":1,"code":"CP01","description":"Keywords must be consistently upper case.","name":"capitalisation.keywords","warning":false}]}]`)
		os.Exit(1)
	case "hang":
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

func fakeBin(t *testing.T, dir, name string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	out.Close()
}

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// lookOnly finds tools in dir only (the fakes on PATH), so a tool installed
// on the machine never leaks into a test.
func lookOnly(dir string) func(string) (string, error) {
	return func(name string) (string, error) {
		p := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			p += ".exe"
		}
		if _, err := os.Stat(p); err != nil {
			return "", toolrun.ErrNotFound
		}
		return p, nil
	}
}

type fakeGit struct{ files []string }

func (g fakeGit) Show(ref, p string) ([]byte, error) { return nil, os.ErrNotExist }
func (g fakeGit) ListFiles() ([]string, error)       { return g.files, nil }

func packsOf(t *testing.T, body string) map[string]manifest.PackConfig {
	t.Helper()
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const dbConfig = `
db:
  enabled: true
  tools: %s
  migrations:
    - {path: supabase/migrations, engine: postgres, migrator: supabase}
    - {path: db/mssql, engine: sqlserver, migrator: flyway}
  small_tables: [public.feature_flags]
  edition: enterprise
  sqlfluff_fail: false
`

func TestConfigValidation(t *testing.T) {
	if err := manifest.ValidatePacks(packsOf(t, fmt.Sprintf(dbConfig, "[squawk]"))); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	bad := map[string]string{
		"engine":   "db: {migrations: [{path: m, engine: mysql}]}",
		"migrator": "db: {migrations: [{path: m, engine: postgres, migrator: liquibase}]}",
		"entry":    "db: {migrations: [{path: m, engine: postgres, dir: x}]}",
		"abs":      "db: {migrations: [{path: /etc, engine: postgres}]}",
		"dotdot":   "db: {migrations: [{path: ../other, engine: postgres}]}",
		"list":     "db: {migrations: {path: m}}",
		"edition":  "db: {edition: datacenter}",
		"small":    "db: {small_tables: public.x}",
		"bool":     "db: {sqlfluff_fail: maybe}",
	}
	for name, body := range bad {
		if err := manifest.ValidatePacks(packsOf(t, body)); err == nil {
			t.Errorf("%s: invalid config accepted", name)
		}
	}
}

func repo(t *testing.T) (string, []packs.ChangedFile, []string) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "supabase/migrations/20240101000000_orders.sql", "CREATE TABLE public.orders (id bigint PRIMARY KEY, status text);\n")
	write(t, root, "supabase/migrations/20240102000000_idx.sql", "SET lock_timeout = '5s';\nCREATE INDEX orders_status_idx ON public.orders (status);\n-- tq:allow PG001: tiny in every environment\nCREATE INDEX orders_id2_idx ON public.orders (id);\n")
	write(t, root, "db/mssql/V1__init.sql", "CREATE TABLE dbo.Orders (Id int NOT NULL)\nGO\n")
	write(t, root, "db/mssql/V2__note.sql", "SET LOCK_TIMEOUT 5000\nALTER TABLE dbo.Orders ADD Note nvarchar(10) NOT NULL\n")
	write(t, root, "db/mssql/U2__note.sql", "ALTER TABLE dbo.Orders DROP COLUMN Note\n")
	tracked := []string{"supabase/migrations/20240101000000_orders.sql", "db/mssql/V1__init.sql"}
	changed := []packs.ChangedFile{
		{Path: "supabase/migrations/20240102000000_idx.sql", Status: 'A'},
		{Path: "db/mssql/V2__note.sql", Status: 'A'},
		{Path: "db/mssql/U2__note.sql", Status: 'A'},
	}
	return root, changed, tracked
}

func input(t *testing.T, tools string, root string, changed []packs.ChangedFile, tracked []string) *packs.Input {
	t.Helper()
	cfg := packs.Merge(packsOf(t, fmt.Sprintf(dbConfig, tools)), nil)
	in := &packs.Input{Root: root, Base: "HEAD", Changed: changed, Config: cfg, Git: fakeGit{tracked}, Budget: time.Now().Add(time.Minute)}
	in.Tools = packs.NewToolRunner(root, cfg)
	in.SetWorkspace("acme", root)
	return in
}

func byID(rep packs.Report) map[string]packs.Result {
	m := map[string]packs.Result{}
	for _, r := range rep.Results {
		m[r.ID] = r
	}
	return m
}

func findings(r packs.Result) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, fmt.Sprintf("%s:%d %s %s", f.File, f.Line, f.Level, f.Rule))
	}
	sort.Strings(out)
	return out
}

func TestBuiltinChecks(t *testing.T) {
	isolate(t)
	root, changed, tracked := repo(t)
	in := input(t, "[]", root, changed, tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}, BuiltinOnly: true})
	res := byID(rep)
	ml := res["db/migration-lint"]
	if ml.Status != packs.Fail || !ml.Required {
		t.Fatalf("migration-lint = %+v", ml)
	}
	want := []string{
		"db/mssql/V2__note.sql:2 error MS002",
		"supabase/migrations/20240102000000_idx.sql:2 error PG001",
	}
	if got := findings(ml); !reflect.DeepEqual(got, want) {
		t.Fatalf("lint findings = %v, want %v", got, want)
	}
	rb := res["db/migration-rollback"]
	if got := findings(rb); !reflect.DeepEqual(got, []string{"supabase/migrations/20240102000000_idx.sql:0 error RB003"}) {
		t.Fatalf("rollback findings = %v (%+v)", got, rb)
	}
	// The spec's migration plan with a Rollback subsection clears RB003.
	write(t, root, "docs/specs/acme-orders.md", "# Orders\n\n## Migration plan\n\n### Rollback\n\ndrop the index\n")
	in.Changed = append(in.Changed, packs.ChangedFile{Path: "docs/specs/acme-orders.md", Status: 'A'})
	rep = packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}, BuiltinOnly: true})
	if rb := byID(rep)["db/migration-rollback"]; rb.Status != packs.OK {
		t.Fatalf("rollback with plan = %+v", rb)
	}
	// The suppression is counted.
	ev, _ := os.ReadFile(packs.EventsFile())
	if !strings.Contains(string(ev), "suppression:pg001") {
		t.Fatalf("no suppression counter in events: %s", ev)
	}
}

func TestChecksSkipWhenNoMigrationChanged(t *testing.T) {
	isolate(t)
	root, _, tracked := repo(t)
	in := input(t, "[]", root, []packs.ChangedFile{{Path: "README.md", Status: 'M'}}, tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}})
	for _, r := range rep.Results {
		if strings.HasPrefix(r.ID, "db/migration") || r.ID == "db/squawk" || r.ID == "db/sqlfluff" {
			t.Fatalf("%s ran with no migration changed", r.ID)
		}
	}
}

func TestExternalChecksWithFakeCLIs(t *testing.T) {
	isolate(t)
	bin := t.TempDir()
	fakeBin(t, bin, "squawk")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_DBTOOL", "squawk")
	t.Setenv("TQ_FAKE_VERSION", "2.70.0")
	argv := filepath.Join(t.TempDir(), "argv")
	t.Setenv("TQ_FAKE_ARGV", argv)
	root, changed, tracked := repo(t)
	// sqlfluff is declared and not installed: its check is a required fail.
	in := input(t, "[squawk, sqlfluff]", root, changed, tracked)
	in.Tools.(*toolrun.Runner).LookPath = lookOnly(bin)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}})
	res := byID(rep)
	sq := res["db/squawk"]
	if sq.Status != packs.Fail || !sq.Required || sq.Tool == nil || sq.Tool.Version != "2.70.0" {
		t.Fatalf("squawk = %+v", sq)
	}
	if got := findings(sq); !reflect.DeepEqual(got, []string{"supabase/migrations/20240102000000_idx.sql:2 error squawk:require-concurrent-index-creation"}) {
		t.Fatalf("squawk findings = %v", got)
	}
	b, _ := os.ReadFile(argv)
	if got := strings.Split(string(b), "\n"); !reflect.DeepEqual(got, []string{"--reporter", "json", "supabase/migrations/20240102000000_idx.sql"}) {
		t.Fatalf("squawk argv = %q (only changed Postgres forward migrations)", got)
	}
	sf := res["db/sqlfluff"]
	if sf.Status != packs.Fail || !sf.Required || sf.Detail != packs.MissingDetail("db") {
		t.Fatalf("declared-missing sqlfluff = %+v", sf)
	}
}

func TestExternalSqlfluffPerDialect(t *testing.T) {
	isolate(t)
	bin := t.TempDir()
	fakeBin(t, bin, "sqlfluff")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_DBTOOL", "sqlfluff")
	t.Setenv("TQ_FAKE_VERSION", "3.2.5")
	argv := filepath.Join(t.TempDir(), "argv")
	t.Setenv("TQ_FAKE_ARGV", argv)
	root, changed, tracked := repo(t)
	in := input(t, "[sqlfluff]", root, changed, tracked)
	in.Tools.(*toolrun.Runner).LookPath = lookOnly(bin)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}})
	sf := byID(rep)["db/sqlfluff"]
	// Style findings are warnings: the check passes.
	if sf.Status != packs.OK || len(sf.Findings) != 2 || sf.Findings[0].Level != packs.LevelWarn {
		t.Fatalf("sqlfluff = %+v", sf)
	}
	b, _ := os.ReadFile(argv) // the last run: tsql over the changed SQL Server forward migration
	if got := strings.Split(string(b), "\n"); !reflect.DeepEqual(got, []string{"lint", "--format", "json", "--dialect", "tsql", "db/mssql/V2__note.sql"}) {
		t.Fatalf("sqlfluff argv = %q", got)
	}
}

func TestHangingToolIsKilledAtItsTimeout(t *testing.T) {
	isolate(t)
	bin := t.TempDir()
	fakeBin(t, bin, "squawk")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_DBTOOL", "hang")
	t.Setenv("TQ_FAKE_VERSION", "2.70.0")
	root, changed, tracked := repo(t)
	cfg := packs.Merge(packsOf(t, "db:\n  tools: [squawk]\n  timeouts: {squawk: 1s}\n  migrations: [{path: supabase/migrations, engine: postgres, migrator: supabase}]\n"), nil)
	in := &packs.Input{Root: root, Base: "HEAD", Changed: changed, Config: cfg, Git: fakeGit{tracked}, Budget: time.Now().Add(time.Minute)}
	r := packs.NewToolRunner(root, cfg)
	r.LookPath = lookOnly(bin)
	in.Tools = r
	start := time.Now()
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}})
	sq := byID(rep)["db/squawk"]
	if sq.Status != packs.Fail || !sq.Required || !strings.Contains(sq.Detail, "timed out") {
		t.Fatalf("hung squawk = %+v", sq)
	}
	if time.Since(start) > 20*time.Second {
		t.Fatalf("the hung tool was not killed at its timeout (%s)", time.Since(start))
	}
}

func TestFlagsAndCrew(t *testing.T) {
	isolate(t)
	cfg := packs.Merge(packsOf(t, fmt.Sprintf(dbConfig, "[]")), nil)
	in := &packs.Input{Config: cfg, Changed: []packs.ChangedFile{{Path: "supabase/migrations/20240103_x.sql", Status: 'A'}}}
	if got := packs.RaiseFlags(in); !reflect.DeepEqual(got, []string{"db_migration"}) {
		t.Fatalf("flags = %v", got)
	}
	in.Changed = []packs.ChangedFile{{Path: "services/api/prisma/migrations/20240101_init/migration.sql", Status: 'A'}}
	if got := packs.RaiseFlags(in); !reflect.DeepEqual(got, []string{"db_migration"}) {
		t.Fatalf("MIGRATION_DIR_RE path: flags = %v", got)
	}
	in.Changed = []packs.ChangedFile{{Path: "src/app.go", Status: 'M'}}
	if got := packs.RaiseFlags(in); len(got) != 0 {
		t.Fatalf("no migration: flags = %v", got)
	}
	for _, f := range []string{"db_migration", "schema", "migration", "rls", "data_loss", "destructive"} {
		if got := packs.Crew(cfg, []string{f}); !reflect.DeepEqual(got, []string{"db-reviewer"}) {
			t.Errorf("crew(%s) = %v", f, got)
		}
	}
	off := packs.Merge(packsOf(t, "db: {enabled: false}"), nil)
	if got := packs.Crew(off, []string{"migration"}); len(got) != 0 {
		t.Fatalf("disabled pack adds reviewers: %v", got)
	}
}

func TestRegisteredChecks(t *testing.T) {
	var ids []string
	for _, c := range packs.Checks("db") {
		ids = append(ids, c.ID)
	}
	for _, want := range []string{"db/migration-lint", "db/migration-rollback", "db/sqlfluff", "db/squawk"} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s not registered (have %v)", want, ids)
		}
	}
}

// stagedGit serves the index (Show ":") from a map.
type stagedGit struct {
	files  []string
	staged map[string]string
}

func (g stagedGit) Show(ref, p string) ([]byte, error) {
	if b, ok := g.staged[p]; ok && ref == ":" {
		return []byte(b), nil
	}
	return nil, os.ErrNotExist
}
func (g stagedGit) ListFiles() ([]string, error) { return g.files, nil }

// One dialect's sqlfluff run fails after the other left warnings: the check
// must fail, not pass with the warnings.
func TestSqlfluffOneDialectFailsOtherWarns(t *testing.T) {
	isolate(t)
	bin := t.TempDir()
	fakeBin(t, bin, "sqlfluff")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_DBTOOL", "sqlfluff-tsql-broken")
	t.Setenv("TQ_FAKE_VERSION", "3.2.5")
	root, changed, tracked := repo(t)
	in := input(t, "[sqlfluff]", root, changed, tracked)
	in.Tools.(*toolrun.Runner).LookPath = lookOnly(bin)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}})
	sf := byID(rep)["db/sqlfluff"]
	if sf.Status != packs.Fail || !sf.Required {
		t.Fatalf("sqlfluff with a failed tsql run = %+v", sf)
	}
}

// An engine alias the validator accepts (supabase, postgresql, mssql …)
// selects the same external checks as the canonical name.
func TestEngineAliasesRunExternalChecks(t *testing.T) {
	isolate(t)
	bin := t.TempDir()
	fakeBin(t, bin, "squawk")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_DBTOOL", "squawk")
	t.Setenv("TQ_FAKE_VERSION", "2.70.0")
	root, changed, tracked := repo(t)
	cfg := packs.Merge(packsOf(t, "db:\n  tools: [squawk]\n  migrations:\n    - {path: supabase/migrations, engine: supabase, migrator: supabase}\n    - {path: db/mssql, engine: mssql, migrator: flyway}\n"), nil)
	in := &packs.Input{Root: root, Base: "HEAD", Changed: changed, Config: cfg, Git: fakeGit{tracked}, Budget: time.Now().Add(time.Minute)}
	r := packs.NewToolRunner(root, cfg)
	r.LookPath = lookOnly(bin)
	in.Tools = r
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}})
	sq, ok := byID(rep)["db/squawk"]
	if !ok || sq.Status != packs.Fail || len(sq.Findings) != 1 {
		t.Fatalf("squawk with engine: supabase = %+v (ran: %v)", sq, ok)
	}
	var engines []string
	for _, m := range lint.Migrations(in.ForPack("db").Settings()) {
		engines = append(engines, m.Engine)
	}
	if !reflect.DeepEqual(engines, []string{"postgres", "sqlserver"}) {
		t.Fatalf("engines = %v, want canonical names", engines)
	}
}

// The same path listed twice (say a repo tq-packs.yaml entry with another
// migrator) reports each finding once.
func TestDuplicateEntryFindingsOnce(t *testing.T) {
	isolate(t)
	root, changed, tracked := repo(t)
	cfg := packs.Merge(packsOf(t, "db:\n  migrations:\n    - {path: supabase/migrations, engine: postgres, migrator: supabase}\n    - {path: supabase/migrations, engine: postgres, migrator: raw}\n"), nil)
	in := &packs.Input{Root: root, Base: "HEAD", Changed: changed, Config: cfg, Git: fakeGit{tracked}, Budget: time.Now().Add(time.Minute)}
	in.Tools = packs.NewToolRunner(root, cfg)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}, BuiltinOnly: true})
	res := byID(rep)
	if got := findings(res["db/migration-lint"]); !reflect.DeepEqual(got, []string{"supabase/migrations/20240102000000_idx.sql:2 error PG001"}) {
		t.Fatalf("lint findings = %v", got)
	}
	if got := findings(res["db/migration-rollback"]); len(got) != 1 {
		t.Fatalf("rollback findings = %v", got)
	}
}

// --staged: the external tools lint the staged blob, not the working tree.
func TestStagedExternalToolSeesTheIndex(t *testing.T) {
	isolate(t)
	bin := t.TempDir()
	fakeBin(t, bin, "squawk")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_DBTOOL", "squawk")
	t.Setenv("TQ_FAKE_VERSION", "2.70.0")
	seen := filepath.Join(t.TempDir(), "seen")
	t.Setenv("TQ_FAKE_SEEN", seen)
	root, changed, tracked := repo(t)
	const idx = "supabase/migrations/20240102000000_idx.sql"
	stagedSQL := "SET lock_timeout = '5s';\nCREATE INDEX staged_idx ON public.orders (status);\n"
	cfg := packs.Merge(packsOf(t, fmt.Sprintf(dbConfig, "[squawk]")), nil)
	in := &packs.Input{Root: root, Staged: true, Changed: changed, Config: cfg, Budget: time.Now().Add(time.Minute),
		Git: stagedGit{files: tracked, staged: map[string]string{idx: stagedSQL}}}
	r := packs.NewToolRunner(root, cfg)
	r.LookPath = lookOnly(bin)
	in.Tools = r
	in.SetWorkspace("acme", root)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"db"}})
	sq := byID(rep)["db/squawk"]
	b, _ := os.ReadFile(seen)
	if string(b) != stagedSQL {
		t.Fatalf("squawk saw %q, want the staged blob (result %+v)", b, sq)
	}
	if got := findings(sq); !reflect.DeepEqual(got, []string{idx + ":2 error squawk:require-concurrent-index-creation"}) {
		t.Fatalf("staged squawk findings = %v", got)
	}
}
