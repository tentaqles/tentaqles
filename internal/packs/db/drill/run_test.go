package drill

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

const pw = "s3cret-tester" // tq:allow-secret fake test fixture

type drillEnv struct {
	t    *testing.T
	home string
	repo string
	in   *packs.Input
	log  string
	vars map[string]string
}

func setupDrill(t *testing.T, block string) *drillEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TQ_HUB_TASK", "TENTAQLES_HEADLESS", "CLAUDECODE", "CLAUDE_PLUGIN_ROOT"} {
		t.Setenv(k, "")
	}
	repo := filepath.Join(home, "acme")
	os.MkdirAll(filepath.Join(repo, "supabase", "migrations"), 0o755)
	os.WriteFile(filepath.Join(repo, "supabase", "migrations", "20260901000000_init.sql"), []byte("create table public.orders(id int);\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "supabase", "migrations", "20261001000000_customers.sql"), []byte("create table public.customers(id int);\n"), 0o644)
	installFakes(t, home)
	log := filepath.Join(home, "argv.jsonl")
	t.Setenv("TQ_FAKE_DRILL_LOG", log)
	wd, _ := os.Getwd()
	fix, _ := filepath.Abs(filepath.Join(wd, "testdata", "dumps"))
	t.Setenv("TQ_FAKE_DRILL_FIX", fix)
	for _, k := range []string{"TQ_FAKE_DRILL_COUNT", "TQ_FAKE_DRILL_FAIL", "TQ_FAKE_DRILL_VERSION"} {
		t.Setenv(k, "")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(block), &doc); err != nil {
		t.Fatal(err)
	}
	en := true
	m := &manifest.Manifest{Packs: map[string]manifest.PackConfig{"db": {Enabled: &en, Raw: *doc.Content[0]}}}
	cfg := packs.Merge(m.Packs, nil)
	in := &packs.Input{Root: repo, Manifest: m, Config: cfg, Tools: packs.NewToolRunner(repo, cfg)}
	in.SetWorkspace("acme", repo)
	e := &drillEnv{t: t, home: home, repo: repo, in: in.ForPack("db"), log: log, vars: map[string]string{}}
	return e
}

func (e *drillEnv) getenv(k string) string { return e.vars[k] }

func (e *drillEnv) run(name string) (Outcome, string, error) {
	e.t.Helper()
	ds, err := Parse(e.in.Settings())
	if err != nil {
		e.t.Fatal(err)
	}
	d, ok := Find(ds, name)
	if !ok {
		e.t.Fatalf("no drill %s", name)
	}
	var out bytes.Buffer
	o, err := Run(context.Background(), Options{In: e.in, Drill: d, Out: &out, Getenv: e.getenv})
	return o, out.String(), err
}

func (e *drillEnv) evidence() []Entry {
	e.t.Helper()
	raw, err := os.ReadFile(filepath.Join(e.repo, filepath.FromSlash(EvidenceFile)))
	if err != nil {
		e.t.Fatal(err)
	}
	es, bad := ParseLog(raw)
	if len(bad) > 0 {
		e.t.Fatalf("bad evidence lines %v:\n%s", bad, raw)
	}
	return es
}

const pgBlock = `
migrations:
  - {path: supabase/migrations, engine: postgres, migrator: supabase}
test_db: {url_env: TEST_DATABASE_URL}
drill:
  - name: main
    engine: postgres
    source: {kind: file, path_env: BACKUP_DUMP_PATH}
    verify: [public.orders, public.customers]
    freshness: 30d
  - name: plain
    engine: postgres
    source: {kind: file, path_env: PLAIN_DUMP_PATH}
    verify: [public.orders]
  - name: fetched
    engine: postgres
    source: {kind: command, command: [fetchdump, --latest]}
    verify: [public.orders]
  - name: supa
    engine: postgres
    source: {kind: supabase-dump}
    verify: [public.orders]
environments:
  dev:  {match: {dotenv: [.env], hosts: [localhost]}, migrate: auto}
  prod: {match: {dotenv: [.env.prod], project_refs: [aaaaaaaaaaaaaaaaaaaa]}, migrate: approve}
`

func archivePath(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "acme.dump")
	if err := os.WriteFile(p, archiveHeader(t, 1, 15, 0, time.Date(2026, 10, 20, 3, 0, 0, 0, time.Local)), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDrillArchiveWritesEvidence(t *testing.T) {
	e := setupDrill(t, pgBlock)
	e.vars["TEST_DATABASE_URL"] = "postgres://tester:" + pw + "@localhost:54322/postgres"
	e.vars["BACKUP_DUMP_PATH"] = archivePath(t, e.home)
	o, out, err := e.run("main")
	if err != nil {
		t.Fatal(err)
	}
	l := o.Line
	if !l.OK || l.FailedStep != "" || l.HostKind != "local" || l.Source != "file" || l.Engine != "postgres" {
		t.Fatalf("line: %+v", l)
	}
	if len(l.Tables) != 2 || l.Tables[0] != (Table{"public.orders", 42}) || l.Schema != (Schema{12, 30}) {
		t.Fatalf("counts: %+v %+v", l.Tables, l.Schema)
	}
	if l.MigrationVersion != "20261001000000" || l.MigrationFiles != "20261001000000" || l.MigrationGap {
		t.Fatalf("version: %+v", l)
	}
	if l.BackupTimeFrom != "dump-header" || l.BackupTakenAt != time.Date(2026, 10, 20, 3, 0, 0, 0, time.Local).UTC().Format(time.RFC3339) {
		t.Fatalf("backup time: %s %s", l.BackupTimeFrom, l.BackupTakenAt)
	}
	if l.ToolVersions["psql"] != "16.2.0" && l.ToolVersions["psql"] != "16.2" {
		t.Fatalf("tool versions: %v", l.ToolVersions)
	}
	es := e.evidence()
	if len(es) != 1 || es[0].Name != "main" {
		t.Fatalf("evidence: %+v", es)
	}
	raw, _ := os.ReadFile(filepath.Join(e.repo, filepath.FromSlash(EvidenceFile)))
	for _, leak := range []string{pw, "localhost", "54322", "tester", "tq_drill_"} {
		if strings.Contains(string(raw), leak) || strings.Contains(out, leak) {
			t.Fatalf("evidence or output names %q:\n%s\n%s", leak, raw, out)
		}
	}
	calls := readCalls(t, e.log)
	var tools []string
	for _, c := range calls {
		if c.Args[0] == "--version" {
			continue
		}
		tools = append(tools, c.Tool)
		for _, a := range c.Args {
			if strings.Contains(a, pw) {
				t.Fatalf("the password reached argv: %v", c.Args)
			}
		}
		if c.Tool == "psql" || c.Tool == "pg_restore" {
			if !c.PW {
				t.Fatalf("%s did not get PGPASSWORD", c.Tool)
			}
		}
	}
	got := strings.Join(tools, " ")
	if !strings.HasPrefix(got, "psql pg_restore psql") || !strings.HasSuffix(got, "psql") {
		t.Fatalf("tool order: %s", got)
	}
	var restore, create, drop []string
	for _, c := range calls {
		j := strings.Join(c.Args, " ")
		switch {
		case c.Tool == "pg_restore":
			restore = c.Args
		case strings.Contains(j, "CREATE DATABASE"):
			create = c.Args
		case strings.Contains(j, "DROP DATABASE"):
			drop = c.Args
		}
	}
	if strings.Join(restore[:3], " ") != "--no-owner --no-acl -d" || !strings.HasPrefix(restore[3], "tq_drill_main_") {
		t.Fatalf("restore argv: %v", restore)
	}
	if create == nil || drop == nil || !strings.Contains(strings.Join(drop, " "), restore[3]) {
		t.Fatalf("create %v drop %v", create, drop)
	}
	// the dump folder is gone without --keep; the log is private
	if _, err := os.Stat(filepath.Join(e.in.StateDir(), "drills", "tmp", o.Scratch)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tmp kept: %v", err)
	}
	if lb, _ := os.ReadFile(o.Log); strings.Contains(string(lb), pw) {
		t.Fatal("the log holds the password")
	}
}

func TestDrillRefusesANonLocalTarget(t *testing.T) {
	for _, url := range []string{
		"postgres://tester:" + pw + "@db.aaaaaaaaaaaaaaaaaaaa.supabase.co:5432/postgres",
		"postgres://tester@10.0.0.5/acme",
		"postgres://tester@localhost/acme?hostaddr=10.0.0.5",
		"postgres://tester@localhost,db.acme.test/acme",
		"postgres://tester@localhost/acme?service=prod",
		"sqlserver://tester@localhost:1433?database=acme", // the wrong engine
	} {
		t.Run(url, func(t *testing.T) {
			e := setupDrill(t, pgBlock)
			e.vars["TEST_DATABASE_URL"] = url
			e.vars["BACKUP_DUMP_PATH"] = archivePath(t, e.home)
			_, _, err := e.run("main")
			if err == nil {
				t.Fatal("ran against a non-local target")
			}
			if strings.Contains(err.Error(), pw) {
				t.Fatalf("error leaks the password: %v", err)
			}
			for _, c := range readCalls(t, e.log) {
				if c.Args[0] != "--version" {
					t.Fatalf("a tool ran before the refusal: %+v", c)
				}
			}
			if _, err := os.Stat(filepath.Join(e.repo, filepath.FromSlash(EvidenceFile))); err == nil {
				t.Fatal("a refused drill wrote evidence")
			}
		})
	}
}

func TestDrillEmptyTableIsNotOK(t *testing.T) {
	e := setupDrill(t, pgBlock)
	e.vars["TEST_DATABASE_URL"] = "postgres://tester@127.0.0.1:54322/postgres"
	e.vars["BACKUP_DUMP_PATH"] = archivePath(t, e.home)
	t.Setenv("TQ_FAKE_DRILL_COUNT", "0")
	o, _, err := e.run("main")
	if err != nil {
		t.Fatal(err)
	}
	if o.Line.OK || o.Line.FailedStep != "verify" {
		t.Fatalf("an empty verified table passed: %+v", o.Line)
	}
	if es := e.evidence(); len(es) != 1 || es[0].OK {
		t.Fatalf("evidence: %+v", es)
	}
}

func TestDrillRestoreFailureRecordsAFailAndDrops(t *testing.T) {
	e := setupDrill(t, pgBlock)
	e.vars["TEST_DATABASE_URL"] = "postgres://tester@localhost/postgres"
	e.vars["BACKUP_DUMP_PATH"] = archivePath(t, e.home)
	t.Setenv("TQ_FAKE_DRILL_FAIL", "--no-owner")
	o, out, err := e.run("main")
	if err != nil {
		t.Fatal(err)
	}
	if o.Line.OK || o.Line.FailedStep != "restore" {
		t.Fatalf("line: %+v", o.Line)
	}
	if strings.Contains(out, "tester@acme.test") {
		t.Fatal("tool output (row data) reached the command output")
	}
	dropped := false
	for _, c := range readCalls(t, e.log) {
		if strings.Contains(strings.Join(c.Args, " "), "DROP DATABASE") {
			dropped = true
		}
	}
	if !dropped {
		t.Fatal("the scratch database was not dropped after a failed restore")
	}
}

func TestDrillPlainDumpAndMigrationGap(t *testing.T) {
	e := setupDrill(t, pgBlock)
	e.vars["TEST_DATABASE_URL"] = "postgres://tester@localhost/postgres"
	p := filepath.Join(e.home, "plain.sql")
	os.WriteFile(p, []byte("--\n-- PostgreSQL database dump\n--\nSET statement_timeout = 0;\n"), 0o600)
	e.vars["PLAIN_DUMP_PATH"] = p
	t.Setenv("TQ_FAKE_DRILL_VERSION", "20260901000000")
	o, _, err := e.run("plain")
	if err != nil {
		t.Fatal(err)
	}
	if !o.Line.OK || o.Line.BackupTimeFrom != "file-mtime" || !o.Line.MigrationGap || o.Line.MigrationVersion != "20260901000000" {
		t.Fatalf("line: %+v", o.Line)
	}
	for _, c := range readCalls(t, e.log) {
		if c.Tool == "pg_restore" {
			t.Fatal("a plain dump went to pg_restore")
		}
	}
}

func TestDrillCommandSource(t *testing.T) {
	e := setupDrill(t, pgBlock)
	e.vars["TEST_DATABASE_URL"] = "postgres://tester@localhost/postgres"
	o, _, err := e.run("fetched")
	if err != nil {
		t.Fatal(err)
	}
	if !o.Line.OK || o.Line.Source != "command" {
		t.Fatalf("line: %+v", o.Line)
	}
	calls := readCalls(t, e.log)
	found := false
	for _, c := range calls {
		if c.Tool == "fetchdump" && c.Args[0] == "--latest" {
			found = true
			if strings.Join(c.Env, ",") != "TQ_DRILL_OUT" {
				t.Fatalf("the fetch command got connection variables: %v", c.Env)
			}
		}
	}
	if !found {
		t.Fatalf("the fetch command did not run: %+v", calls)
	}
}

func TestDrillSupabaseDumpTakesDataAndRoles(t *testing.T) {
	e := setupDrill(t, pgBlock)
	e.vars["TEST_DATABASE_URL"] = "postgres://tester@localhost:54322/postgres"
	os.MkdirAll(filepath.Join(e.repo, "supabase", ".temp"), 0o755)
	os.WriteFile(filepath.Join(e.repo, "supabase", ".temp", "project-ref"), []byte("aaaaaaaaaaaaaaaaaaaa\n"), 0o644)
	o, _, err := e.run("supa")
	if err != nil {
		t.Fatal(err)
	}
	if !o.Line.OK || o.Line.BackupTimeFrom != "dump-time" {
		t.Fatalf("line: %+v", o.Line)
	}
	var dumps, restores []string
	for _, c := range readCalls(t, e.log) {
		j := strings.Join(c.Args, " ")
		switch {
		case c.Tool == "supabase" && c.Args[0] == "db":
			dumps = append(dumps, j)
		case c.Tool == "psql" && strings.Contains(j, " -f "):
			restores = append(restores, filepath.Base(c.Args[len(c.Args)-1]))
		}
	}
	if len(dumps) != 3 || !strings.Contains(dumps[0], "--role-only") || strings.Contains(dumps[1], "-only") || !strings.Contains(dumps[2], "--data-only") {
		t.Fatalf("dumps: %v", dumps)
	}
	for _, d := range dumps {
		if !strings.Contains(d, "--linked") {
			t.Fatalf("dump without --linked: %s", d)
		}
	}
	if strings.Join(restores, " ") != "roles.sql schema.sql data.sql" {
		t.Fatalf("restore order: %v", restores)
	}
}

func TestDrillSupabaseRefusesAnUndeclaredProject(t *testing.T) {
	e := setupDrill(t, pgBlock)
	e.vars["TEST_DATABASE_URL"] = "postgres://tester@localhost:54322/postgres"
	e.vars["SUPABASE_PROJECT_ID"] = "bbbbbbbbbbbbbbbbbbbb"
	_, _, err := e.run("supa")
	if err == nil || !strings.Contains(err.Error(), "project_refs") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range readCalls(t, e.log) {
		if c.Tool == "supabase" && c.Args[0] != "--version" {
			t.Fatal("dumped an undeclared project")
		}
	}
}

const msBlock = `
migrations:
  - {path: db/mssql, engine: sqlserver, migrator: efcore}
test_db: {url_env: TEST_DATABASE_URL}
drill:
  - name: bacpac
    engine: sqlserver
    source: {kind: azure-sql-bacpac, path_env: BACPAC_PATH}
    verify: [dbo.Orders]
  - name: bak
    engine: sqlserver
    source: {kind: file, path_env: BAK_PATH}
    verify: [dbo.Orders]
`

func TestDrillSQLServerBacpac(t *testing.T) {
	e := setupDrill(t, msBlock)
	e.vars["TEST_DATABASE_URL"] = "sqlserver://tester:" + pw + "@localhost:1433?database=master"
	p := filepath.Join(e.home, "acme.bacpac")
	os.WriteFile(p, []byte("PK fake bacpac"), 0o600)
	e.vars["BACPAC_PATH"] = p
	o, _, err := e.run("bacpac")
	if err != nil {
		t.Fatal(err)
	}
	if !o.Line.OK || o.Line.Tables[0].Count != 42 {
		t.Fatalf("line: %+v", o.Line)
	}
	var imp []string
	for _, c := range readCalls(t, e.log) {
		if c.Tool == "sqlpackage" && c.Args[0] != "--version" {
			imp = c.Args
		}
		if c.Tool == "sqlcmd" && c.Args[0] != "--version" && !c.PW {
			t.Fatalf("sqlcmd without SQLCMDPASSWORD: %v", c.Args)
		}
	}
	if len(imp) == 0 || imp[0] != "/a:Import" || !strings.HasPrefix(imp[3], "/tdn:tq_drill_bacpac_") {
		t.Fatalf("sqlpackage argv: %v", imp)
	}
}

func TestDrillSQLServerBak(t *testing.T) {
	e := setupDrill(t, msBlock)
	e.vars["TEST_DATABASE_URL"] = "Server=tcp:127.0.0.1,1433;Database=master;User ID=tester;Password=" + pw
	e.vars["BAK_PATH"] = "/var/opt/mssql/backup/acme.bak"
	o, _, err := e.run("bak")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 20, 3, 0, 0, 0, time.Local).UTC().Format(time.RFC3339)
	if !o.Line.OK || o.Line.BackupTimeFrom != "backup-header" || o.Line.BackupTakenAt != want {
		t.Fatalf("line: %+v", o.Line)
	}
	var restore string
	for _, c := range readCalls(t, e.log) {
		j := strings.Join(c.Args, " ")
		if strings.Contains(j, "RESTORE DATABASE") {
			restore = j
		}
	}
	if !strings.Contains(restore, "MOVE N'acme' TO N'/var/opt/mssql/data/tq_drill_bak_") || !strings.Contains(restore, "MOVE N'acme_log' TO N'/var/opt/mssql/log/") {
		t.Fatalf("restore: %s", restore)
	}
}
