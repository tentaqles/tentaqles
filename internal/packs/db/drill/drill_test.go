package drill

import (
	"encoding/binary"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update-drill", false, "rewrite the drill golden files")

func node(t *testing.T, src string) yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return *doc.Content[0]
}

func TestValidateDrill(t *testing.T) {
	ok := `
- name: main
  engine: postgres
  source: {kind: file, path_env: BACKUP_DUMP_PATH}
  verify: [public.orders, customers]
  freshness: 30d
  timeout: 20m
- name: supa
  engine: postgres
  source: {kind: supabase-dump}
- name: ms-bacpac
  engine: sqlserver
  source: {kind: azure-sql-bacpac, path_env: BACPAC}
- name: fetch
  engine: postgres
  source: {kind: command, command: [scripts/fetch-dump, --latest]}
`
	if err := Validate(node(t, ok)); err != nil {
		t.Fatal(err)
	}
	ds, _ := parse(func() *yaml.Node { n := node(t, ok); return &n }())
	if len(ds) != 4 || ds[0].Freshness != 30*24*time.Hour || ds[0].Timeout != 20*time.Minute || ds[1].Freshness != DefaultFreshness || ds[1].Timeout != DefaultTimeout {
		t.Fatalf("parsed: %+v", ds)
	}
	bad := map[string]string{
		"unknown kind":        "- {name: a, engine: postgres, source: {kind: s3}}",
		"no path_env":         "- {name: a, engine: postgres, source: {kind: file}}",
		"command string":      "- {name: a, engine: postgres, source: {kind: command, command: fetch.sh}}",
		"bad name":            "- {name: Main, engine: postgres, source: {kind: supabase-dump}}",
		"duplicate":           "- {name: a, engine: postgres, source: {kind: supabase-dump}}\n- {name: a, engine: postgres, source: {kind: supabase-dump}}",
		"supabase sqlserver":  "- {name: a, engine: sqlserver, source: {kind: supabase-dump}}",
		"bacpac postgres":     "- {name: a, engine: postgres, source: {kind: azure-sql-bacpac, path_env: X}}",
		"bad table":           "- {name: a, engine: postgres, source: {kind: supabase-dump}, verify: ['orders; drop table x']}",
		"freshness too long":  "- {name: a, engine: postgres, source: {kind: supabase-dump}, freshness: 400d}",
		"timeout over cap":    "- {name: a, engine: postgres, source: {kind: supabase-dump}, timeout: 2h}",
		"unknown engine":      "- {name: a, engine: mysql, source: {kind: supabase-dump}}",
		"unknown key":         "- {name: a, engine: postgres, source: {kind: supabase-dump}, host: prod}",
		"path_env not a name": "- {name: a, engine: postgres, source: {kind: file, path_env: '$X'}}",
		"not a list":          "{name: a}",
		"no source":           "- {name: a, engine: postgres}",
	}
	for name, src := range bad {
		if err := Validate(node(t, src)); err == nil || !strings.Contains(err.Error(), "line ") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestEvidenceGolden(t *testing.T) {
	l := Line{Name: "main", Engine: "postgres", Source: "file", BackupTakenAt: "2026-10-20T03:00:00Z", BackupTimeFrom: "dump-header",
		RestoredAt: "2026-10-28T10:00:00Z", DurationS: 95, OK: true, Tables: []Table{{"public.orders", 1200}, {"public.customers", 300}},
		Schema: Schema{12, 30}, MigrationVersion: "20261001000000", MigrationFiles: "20261001000000",
		ToolVersions: map[string]string{"pg_restore": "16.2.0", "psql": "16.2.0"}, HostKind: "local"}
	b, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "golden", "evidence-ok.json")
	if *update {
		os.MkdirAll(filepath.Dir(golden), 0o755)
		os.WriteFile(golden, append(b, '\n'), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(want)) != string(b) {
		t.Fatalf("golden mismatch:\n got %s\nwant %s", b, want)
	}
}

func TestAppendAndFreshness(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, filepath.FromSlash(EvidenceFile))
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"name":"old"`), 0o644) // a truncated line without its newline
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	mk := func(name string, ok bool, restored, backup time.Time) Line {
		return Line{Name: name, Engine: "postgres", OK: ok, RestoredAt: restored.Format(time.RFC3339), BackupTakenAt: backup.Format(time.RFC3339), HostKind: "local"}
	}
	day := 24 * time.Hour
	for _, l := range []Line{
		mk("main", true, now.Add(-40*day), now.Add(-41*day)),
		mk("main", true, now.Add(-2*day), now.Add(-3*day)),
		mk("main", false, now.Add(-1*day), now.Add(-1*day)), // newer, but failed
		mk("oldbackup", true, now.Add(-1*day), now.Add(-60*day)),
		mk("stale", true, now.Add(-31*day), now.Add(-31*day)),
		mk("future", true, now.Add(2*day), now.Add(-1*day)),
	} {
		if err := Append(root, l); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(p)
	es, bad := ParseLog(raw)
	if len(bad) != 1 || bad[0] != 1 || len(es) != 6 {
		t.Fatalf("parse: %d entries, bad %v\n%s", len(es), bad, raw)
	}
	d := func(n string) Drill { return Drill{Name: n, Freshness: 30 * day} }
	if f := Check(es, d("main"), now); !f.Fresh || Days(f.Age) != "2d" {
		t.Fatalf("main: %+v", f)
	}
	for _, n := range []string{"oldbackup", "stale", "future", "missing"} {
		if f := Check(es, d(n), now); f.Fresh || f.Why == "" {
			t.Errorf("%s fresh: %+v", n, f)
		}
	}
	// the binding changes with a new ok line, not with a failed one
	b1 := Binding(raw, []Drill{d("main"), d("stale")})
	Append(root, mk("main", false, now, now))
	raw2, _ := os.ReadFile(p)
	if Binding(raw2, []Drill{d("stale"), d("main")}) != b1 {
		t.Fatal("a failed line (or the drill order) changed the binding")
	}
	Append(root, mk("main", true, now, now))
	raw3, _ := os.ReadFile(p)
	if Binding(raw3, []Drill{d("main"), d("stale")}) == b1 {
		t.Fatal("a new ok line did not change the binding")
	}
	if Binding(raw3, nil) != BindingNone || Binding(nil, []Drill{d("main")}) == BindingNone {
		t.Fatal("binding of no drill / no line")
	}
}

// archiveHeader builds the start of a pg_dump custom archive.
func archiveHeader(t *testing.T, vmaj, vmin, vrev byte, at time.Time) []byte {
	t.Helper()
	b := []byte("PGDMP")
	b = append(b, vmaj, vmin, vrev, 4, 8, 1)
	wint := func(v int) {
		sign := byte(0)
		if v < 0 {
			sign, v = 1, -v
		}
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v))
		b = append(b, sign)
		b = append(b, buf...)
	}
	if vmin >= 15 {
		b = append(b, 1) // gzip
	} else {
		wint(-1)
	}
	for _, v := range []int{at.Second(), at.Minute(), at.Hour(), at.Day(), int(at.Month()) - 1, at.Year() - 1900, 0} {
		wint(v)
	}
	return append(b, []byte("....acme")...)
}

func TestPGDumpHeader(t *testing.T) {
	at := time.Date(2026, 10, 20, 3, 4, 5, 0, time.Local)
	for _, v := range [][3]byte{{1, 15, 0}, {1, 14, 0}, {1, 16, 0}} {
		f, got, ok := PGDumpHeader(archiveHeader(t, v[0], v[1], v[2], at))
		if f != DumpArchive || !ok || !got.Equal(at) {
			t.Errorf("%v: %v %v %v", v, f, got, ok)
		}
	}
	if f, _, ok := PGDumpHeader([]byte("PGDMP\x01")); f != DumpArchive || ok {
		t.Fatal("a truncated header")
	}
	if f, _, _ := PGDumpHeader([]byte("--\n-- PostgreSQL database dump\n")); f != DumpPlain {
		t.Fatal("plain")
	}
	if f, _, _ := PGDumpHeader([]byte{0x1f, 0x8b, 0x08}); f != DumpUnknown {
		t.Fatal("gzip is unknown")
	}
}

func TestParseConnAndLocal(t *testing.T) {
	cases := []struct {
		url   string
		local bool
		host  string
		db    string
	}{
		{"postgres://tester:x@localhost:54322/postgres", true, "localhost", "postgres"},
		{"postgresql://tester@127.0.0.1/acme", true, "127.0.0.1", "acme"},
		{"postgres://tester@[::1]:5432/acme", true, "::1", "acme"},
		{"postgres:///acme?host=/var/run/postgresql", true, "/var/run/postgresql", "acme"},
		{"postgres://tester@db/acme", true, "db", "acme"}, // listed in test_db.hosts
		{"postgres://tester@db.aaaaaaaaaaaaaaaaaaaa.supabase.co/postgres", false, "db.aaaaaaaaaaaaaaaaaaaa.supabase.co", "postgres"},
		{"postgres://tester@localhost:5432,10.1.1.1:5432/acme", false, "localhost", "acme"},
		{"postgres://tester@localhost/acme?hostaddr=192.168.1.4", false, "localhost", "acme"},
		{"sqlserver://tester@localhost:1433?database=acme", true, "localhost", "acme"},
		{"sqlserver://tester@acme-sql.database.windows.net?database=acme", false, "acme-sql.database.windows.net", "acme"},
		{"Server=tcp:.,1433;Database=acme;User ID=tester", true, ".", "acme"},
		{"Server=(local)\\SQLEXPRESS;Database=acme", true, "(local)\\SQLEXPRESS", "acme"},
		{"Data Source=acme-sql.database.windows.net;Initial Catalog=acme", false, "acme-sql.database.windows.net", "acme"},
	}
	for _, c := range cases {
		got, err := ParseConn(c.url)
		if err != nil {
			t.Errorf("%s: %v", c.url, err)
			continue
		}
		if got.Local([]string{"db"}) != c.local || got.Host() != c.host || got.Database != c.db {
			t.Errorf("%s: local %v host %q db %q", c.url, got.Local([]string{"db"}), got.Host(), got.Database)
		}
	}
	for _, s := range []string{"mysql://x@localhost/a", "host=localhost dbname=x", "postgres://x@localhost/a?service=prod"} {
		if _, err := ParseConn(s); err == nil {
			t.Errorf("%s parsed", s)
		}
	}
}

func TestFileVersions(t *testing.T) {
	names := []string{"20240101000000_init.sql", "20261001000000_b.sql", "README.md", "0009_x.up.sql"}
	if v := NewestFileVersion("supabase", names); v != "20261001000000" {
		t.Fatal(v)
	}
	if v := NewestFileVersion("flyway", []string{"V1_2__a.sql", "V1_10__b.sql", "V1_9__c.sql", "R__view.sql"}); v != "1.10" {
		t.Fatal(v)
	}
	if v := NewestFileVersion("prisma", []string{"20240101000000_init", "migration_lock.toml", "20250101000000_two"}); v != "20250101000000_two" {
		t.Fatal(v)
	}
	if v := NewestFileVersion("efcore", []string{"20240101000000_Init.cs", "20240101000000_Init.Designer.cs", "AcmeContextModelSnapshot.cs"}); v != "20240101000000_Init" {
		t.Fatal(v)
	}
	if NewestFileVersion("alembic", []string{"abc123_init.py"}) != "" || VersionQuery("postgres", "raw") != "" || VersionQuery("sqlserver", "efcore") == "" {
		t.Fatal("unknown migrators")
	}
}

func checkInput(t *testing.T, block string, log string) *packs.Input {
	t.Helper()
	root := t.TempDir()
	if log != "" {
		p := filepath.Join(root, filepath.FromSlash(EvidenceFile))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(log), 0o644)
	}
	en := true
	m := &manifest.Manifest{Packs: map[string]manifest.PackConfig{"db": {Enabled: &en, Raw: node(t, block)}}}
	in := &packs.Input{Root: root, Manifest: m, Config: packs.Merge(m.Packs, nil)}
	return in.ForPack("db")
}

func TestRestoreDrillCheck(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	prev := Now
	Now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { Now = prev })
	block := "drill:\n  - {name: main, engine: postgres, source: {kind: supabase-dump}, freshness: 7d}\n  - {name: second, engine: postgres, source: {kind: supabase-dump}}\n"
	fresh := `{"name":"main","ok":true,"restored_at":"2026-10-27T10:00:00Z","backup_taken_at":"2026-10-27T09:00:00Z","host_kind":"local"}
{"name":"second","ok":true,"restored_at":"2026-10-10T10:00:00Z","backup_taken_at":"2026-10-10T09:00:00Z","host_kind":"local"}
`
	if r := RunCheck(checkInput(t, block, fresh)); r.Status != packs.OK {
		t.Fatalf("fresh: %+v", r)
	}
	stale := strings.Replace(fresh, "2026-10-27", "2026-10-01", 2)
	r := RunCheck(checkInput(t, block, stale+"not json\n"))
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "main") || strings.Contains(r.Detail, "second") {
		t.Fatalf("stale: %+v", r)
	}
	var stalef, badf bool
	for _, f := range r.Findings {
		stalef = stalef || (f.Rule == "drill-stale" && f.Level == packs.LevelError && strings.Contains(f.Message, "tq pack db drill main"))
		badf = badf || (f.Rule == "drill-line" && f.Line == 3 && f.Level == packs.LevelWarn)
	}
	if !stalef || !badf {
		t.Fatalf("findings: %+v", r.Findings)
	}
	// a remote host_kind never counts
	remote := strings.ReplaceAll(fresh, `"local"`, `"remote"`)
	if r := RunCheck(checkInput(t, block, remote)); r.Status != packs.Fail {
		t.Fatalf("remote: %+v", r)
	}
	if r := RunCheck(checkInput(t, block, "")); r.Status != packs.Fail {
		t.Fatalf("no log: %+v", r)
	}
	if r := RunCheck(checkInput(t, "migrations: []\n", "")); r.Status != packs.Skip {
		t.Fatalf("no drill: %+v", r)
	}
}
