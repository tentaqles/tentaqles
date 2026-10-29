package pgtap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"gopkg.in/yaml.v3"
)

// TestMain doubles as the fake pg_prove and supabase: a copy of the test
// binary run with TQ_FAKE_PGTAP set prints TQ_FAKE_OUT, exits
// TQ_FAKE_EXIT, and records its argv and environment.
func TestMain(m *testing.M) {
	if mode := os.Getenv("TQ_FAKE_PGTAP"); mode != "" {
		fake(mode)
		return
	}
	os.Exit(m.Run())
}

func fake(mode string) {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(mode + " 3.36")
		os.Exit(0)
	}
	if f := os.Getenv("TQ_FAKE_ARGV"); f != "" {
		af, _ := os.OpenFile(f, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		fmt.Fprintln(af, strings.Join(args, " "))
		af.Close()
	}
	if f := os.Getenv("TQ_FAKE_ENV"); f != "" {
		var keep []string
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "PG") {
				keep = append(keep, e)
			}
		}
		_ = os.WriteFile(f, []byte(strings.Join(keep, "\n")), 0o600)
	}
	if len(args) > 0 && args[0] == "start" {
		_ = os.WriteFile(os.Getenv("TQ_FAKE_STARTED"), []byte("up"), 0o600)
		os.Exit(0)
	}
	if mode == "hang" {
		time.Sleep(30 * time.Second)
	}
	if f := os.Getenv("TQ_FAKE_OUT"); f != "" {
		b, _ := os.ReadFile(f)
		os.Stdout.Write(b)
	}
	if os.Getenv("TQ_FAKE_ECHO_SECRET") != "" {
		fmt.Println("connection to " + os.Getenv("PGPASSWORD") + " failed")
	}
	code := 0
	fmt.Sscanf(os.Getenv("TQ_FAKE_EXIT"), "%d", &code)
	os.Exit(code)
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

const secret = "s3cret-pw-acme" // tq:allow-secret fake test fixture

type env struct {
	root, bin, argv, envf, started string
	in                             *packs.Input
}

func setup(t *testing.T, cfg string, tools ...string) *env {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PGHOSTADDR", "")
	t.Setenv("PGSERVICE", "")
	e := &env{root: t.TempDir(), bin: t.TempDir()}
	for _, tl := range tools {
		fakeBin(t, e.bin, tl)
	}
	t.Setenv("PATH", e.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	e.argv = filepath.Join(t.TempDir(), "argv")
	e.envf = filepath.Join(t.TempDir(), "env")
	e.started = filepath.Join(t.TempDir(), "started")
	t.Setenv("TQ_FAKE_ARGV", e.argv)
	t.Setenv("TQ_FAKE_ENV", e.envf)
	t.Setenv("TQ_FAKE_STARTED", e.started)
	var pc map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(cfg), &pc); err != nil {
		t.Fatal(err)
	}
	c := packs.Merge(pc, nil)
	e.in = &packs.Input{Root: e.root, Config: c, Budget: time.Now().Add(time.Minute)}
	r := packs.NewToolRunner(e.root, c)
	r.LookPath = lookOnly(e.bin)
	e.in.Tools = r
	e.in.SetWorkspace("acme", e.root)
	e.in = e.in.ForPack("db")
	writeFile(t, e.root, "supabase/tests/database/rls_notes.test.sql", "select plan(1);\n")
	return e
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// up reports which addresses answer: everything, nothing, or only after the
// fake start ran.
func dialer(t *testing.T, e *env, mode string) *[]string {
	t.Helper()
	var dialed []string
	old := Dial
	Dial = func(ctx context.Context, network, addr string) error {
		dialed = append(dialed, network+" "+addr)
		switch mode {
		case "up":
			return nil
		case "after-start":
			if _, err := os.Stat(e.started); err == nil {
				return nil
			}
		}
		return errors.New("refused")
	}
	t.Cleanup(func() { Dial = old })
	return &dialed
}

func opts(e *env, mode Mode) Options {
	return Options{Mode: mode, TestsDir: "supabase/tests", TestFiles: []string{"supabase/tests/database/rls_notes.test.sql"}}
}

const pgCfg = "db:\n  tools: [pg_prove]\n  test_db: {url_env: TEST_DATABASE_URL}\n"

func TestPgProveRunsAndReportsFailures(t *testing.T) {
	e := setup(t, pgCfg, "pg_prove")
	t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
	t.Setenv("TQ_FAKE_OUT", filepath.Join("testdata", "pg_prove_fail.txt"))
	abs, _ := filepath.Abs(filepath.Join("testdata", "pg_prove_fail.txt"))
	t.Setenv("TQ_FAKE_OUT", abs)
	t.Setenv("TQ_FAKE_EXIT", "1")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester:"+secret+"@localhost:54322/postgres") // tq:allow-secret fake test fixture
	dialed := dialer(t, e, "up")
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.Fail || len(r.Findings) != 6 {
		t.Fatalf("result = %+v", r)
	}
	f := r.Findings[0]
	if f.File != "supabase/tests/database/rls_notes.test.sql" || f.Line != 2 || f.Rule != "pgtap:not-ok" || f.Level != packs.LevelError || !strings.Contains(f.Message, "a cannot write as b") {
		t.Fatalf("finding = %+v", f)
	}
	if !reflect.DeepEqual(*dialed, []string{"tcp localhost:54322"}) {
		t.Fatalf("dialed = %v", *dialed)
	}
	argv, _ := os.ReadFile(e.argv)
	if got := strings.TrimSpace(string(argv)); got != "--ext .sql -r --failures --host localhost --port 54322 --dbname postgres --username tester supabase/tests" {
		t.Fatalf("argv = %q", got)
	}
	if strings.Contains(string(argv), secret) {
		t.Fatal("the password reached argv")
	}
	envb, _ := os.ReadFile(e.envf)
	if !strings.Contains(string(envb), "PGPASSWORD="+secret) || !strings.Contains(string(envb), "PGHOST=localhost") {
		t.Fatalf("child env = %q", envb)
	}
	for _, fd := range r.Findings {
		if strings.Contains(fd.Message, secret) {
			t.Fatal("secret in a finding")
		}
	}
}

func TestPgProvePass(t *testing.T) {
	e := setup(t, pgCfg, "pg_prove")
	t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
	abs, _ := filepath.Abs(filepath.Join("testdata", "pg_prove_ok.txt"))
	t.Setenv("TQ_FAKE_OUT", abs)
	t.Setenv("TEST_DATABASE_URL", "postgres://tester@127.0.0.1:5432/acme")
	dialer(t, e, "up")
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.OK || len(r.Findings) != 0 || !strings.Contains(r.Detail, "12 test(s) in 2 file(s)") {
		t.Fatalf("result = %+v", r)
	}
}

// A non-local target fails before anything connects or runs.
func TestNotLocalFailsBeforeConnecting(t *testing.T) {
	for _, url := range []string{
		"postgres://tester:" + secret + "@db.abcdefghijklmnop.supabase.co:5432/postgres",
		"postgres://tester@10.0.0.5/acme",
		"postgres://tester@localhost/acme?hostaddr=10.0.0.5",
	} {
		e := setup(t, pgCfg, "pg_prove")
		t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
		t.Setenv("TEST_DATABASE_URL", url)
		dialed := dialer(t, e, "up")
		r := Run(context.Background(), e.in, opts(e, PgProve))
		if r.Status != packs.Fail || !strings.Contains(r.Detail, "test_db is not local") {
			t.Fatalf("%s: result = %+v", url, r)
		}
		if len(*dialed) != 0 {
			t.Fatalf("dialed %v", *dialed)
		}
		if _, err := os.Stat(e.argv); err == nil {
			t.Fatal("pg_prove ran")
		}
		if strings.Contains(r.Detail, secret) || strings.Contains(r.Detail, "supabase.co") {
			t.Fatalf("detail leaks the target: %s", r.Detail)
		}
	}
}

func TestComposeServiceIsLocalWhenListed(t *testing.T) {
	e := setup(t, "db:\n  tools: [pg_prove]\n  test_db: {url_env: TEST_DATABASE_URL, hosts: [db]}\n", "pg_prove")
	t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester@db:5432/acme")
	dialer(t, e, "up")
	if r := Run(context.Background(), e.in, opts(e, PgProve)); r.Status != packs.OK {
		t.Fatalf("result = %+v", r)
	}
}

// The URL comes from the repo .env through the dotenv reader when the
// variable is not in the environment.
func TestURLFromDotenv(t *testing.T) {
	e := setup(t, pgCfg, "pg_prove")
	t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
	t.Setenv("TEST_DATABASE_URL", "")
	writeFile(t, e.root, ".env", "TEST_DATABASE_URL=postgres://tester@localhost:6543/acme\n")
	dialed := dialer(t, e, "up")
	if r := Run(context.Background(), e.in, opts(e, PgProve)); r.Status != packs.OK {
		t.Fatalf("result = %+v", r)
	}
	if !reflect.DeepEqual(*dialed, []string{"tcp localhost:6543"}) {
		t.Fatalf("dialed = %v", *dialed)
	}
}

func TestMissingURL(t *testing.T) {
	e := setup(t, pgCfg, "pg_prove")
	t.Setenv("TEST_DATABASE_URL", "")
	dialer(t, e, "up")
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "TEST_DATABASE_URL is not set") {
		t.Fatalf("result = %+v", r)
	}
}

func TestNoTestDBConfigured(t *testing.T) {
	e := setup(t, "db:\n  tools: [pg_prove]\n", "pg_prove")
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "packs.db.test_db") {
		t.Fatalf("declared: %+v", r)
	}
	e = setup(t, "db:\n  enabled: true\n", "pg_prove")
	if r := Run(context.Background(), e.in, opts(e, PgProve)); r.Status != packs.Skip {
		t.Fatalf("undeclared: %+v", r)
	}
}

func TestEnvironmentRedirectRefused(t *testing.T) {
	e := setup(t, pgCfg, "pg_prove")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester@localhost/acme")
	t.Setenv("PGHOSTADDR", "10.0.0.5")
	dialer(t, e, "up")
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "PGHOSTADDR") {
		t.Fatalf("result = %+v", r)
	}
}

func TestStartWhenDown(t *testing.T) {
	e := setup(t, "db:\n  tools: [pg_prove]\n  test_db: {url_env: TEST_DATABASE_URL, start: supabase start}\n", "pg_prove", "supabase")
	t.Setenv("TQ_FAKE_PGTAP", "fake")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester@localhost:54322/postgres")
	dialed := dialer(t, e, "after-start")
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.OK {
		t.Fatalf("result = %+v", r)
	}
	argv, _ := os.ReadFile(e.argv)
	lines := strings.Split(strings.TrimSpace(string(argv)), "\n")
	if len(lines) != 2 || lines[0] != "start" {
		t.Fatalf("runs = %q", lines)
	}
	if len(*dialed) != 2 {
		t.Fatalf("dialed = %v", *dialed)
	}
	// Down, and no start: a fail, nothing runs.
	e = setup(t, pgCfg, "pg_prove")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester@localhost:54322/postgres")
	dialer(t, e, "down")
	r = Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "does not answer") {
		t.Fatalf("down: %+v", r)
	}
}

func TestSupabaseMode(t *testing.T) {
	e := setup(t, "db:\n  tools: [supabase]\n", "supabase")
	t.Setenv("TQ_FAKE_PGTAP", "supabase")
	abs, _ := filepath.Abs(filepath.Join("testdata", "supabase_fail.txt"))
	t.Setenv("TQ_FAKE_OUT", abs)
	t.Setenv("TQ_FAKE_EXIT", "1")
	dialed := dialer(t, e, "up")
	o := opts(e, Supabase)
	o.SupabasePort = 54329
	r := Run(context.Background(), e.in, o)
	if r.Status != packs.Fail || len(r.Findings) != 1 || r.Findings[0].File != "supabase/tests/database/rls_notes.test.sql" || r.Findings[0].Line != 3 {
		t.Fatalf("result = %+v", r)
	}
	if !reflect.DeepEqual(*dialed, []string{"tcp 127.0.0.1:54329"}) {
		t.Fatalf("dialed = %v", *dialed)
	}
	argv, _ := os.ReadFile(e.argv)
	if got := strings.TrimSpace(string(argv)); got != "test db --local" {
		t.Fatalf("argv = %q", got)
	}
	// Not running and no start: a fail that names supabase start.
	e = setup(t, "db:\n  tools: [supabase]\n", "supabase")
	dialer(t, e, "down")
	if r := Run(context.Background(), e.in, opts(e, Supabase)); r.Status != packs.Fail || !strings.Contains(r.Detail, "supabase start") {
		t.Fatalf("down: %+v", r)
	}
	// A non-local test_db fails in Supabase mode too.
	e = setup(t, "db:\n  tools: [supabase]\n  test_db: {url_env: TEST_DATABASE_URL}\n", "supabase")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester@db.abcdefghijklmnop.supabase.co/postgres")
	dialer(t, e, "up")
	if r := Run(context.Background(), e.in, opts(e, Supabase)); r.Status != packs.Fail || !strings.Contains(r.Detail, "not local") {
		t.Fatalf("remote: %+v", r)
	}
}

// A tool that fails without TAP output fails the check, with the redacted
// tail, and never echoes the password.
func TestRunFailureIsRedacted(t *testing.T) {
	e := setup(t, pgCfg, "pg_prove")
	t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
	t.Setenv("TQ_FAKE_EXIT", "2")
	t.Setenv("TQ_FAKE_ECHO_SECRET", "1")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester:"+secret+"@localhost/acme") // tq:allow-secret fake test fixture
	dialer(t, e, "up")
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.Fail || len(r.Findings) != 1 || r.Findings[0].Rule != "pg_prove:run-failed" {
		t.Fatalf("result = %+v", r)
	}
	if strings.Contains(r.Detail, secret) || strings.Contains(r.Findings[0].Message, secret) {
		t.Fatalf("secret leaked: %+v", r)
	}
}

func TestHangIsKilled(t *testing.T) {
	e := setup(t, "db:\n  tools: [pg_prove]\n  timeouts: {pg_prove: 1s}\n  test_db: {url_env: TEST_DATABASE_URL}\n", "pg_prove")
	t.Setenv("TQ_FAKE_PGTAP", "hang")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester@localhost/acme")
	dialer(t, e, "up")
	start := time.Now()
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "timed out") || time.Since(start) > 20*time.Second {
		t.Fatalf("result = %+v after %s", r, time.Since(start))
	}
}

func TestValidateTestDB(t *testing.T) {
	ok := []string{
		"{url_env: TEST_DATABASE_URL}",
		"{start: supabase start}",
		"{url_env: TEST_DATABASE_URL, start: docker compose up -d db, hosts: [db], dotenv: [.env.test]}",
	}
	for _, s := range ok {
		if err := ValidateTestDB(yamlNode(t, s)); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	bad := []string{
		"{}", "[x]", "{url_env: 'not a name'}", "{url: x}", "{start: ''}", "{hosts: db, url_env: X}",
		"{url_env: X, dotenv: [secrets.txt]}", "{url_env: X, dotenv: [../.env]}", "{url_env: X, hosts: ['a b']}",
	}
	for _, s := range bad {
		if err := ValidateTestDB(yamlNode(t, s)); err == nil {
			t.Errorf("%s accepted", s)
		}
	}
}

func yamlNode(t *testing.T, s string) yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(s), &n); err != nil {
		t.Fatal(err)
	}
	return *n.Content[0]
}
