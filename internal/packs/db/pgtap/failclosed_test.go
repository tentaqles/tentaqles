package pgtap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

func fakeOut(t *testing.T, name string) {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TQ_FAKE_OUT", abs)
}

// A run in which no test ran, or the harness did not say PASS, fails
// closed, whatever the exit code.
func TestNoTestsOrNoPassFails(t *testing.T) {
	for name, want := range map[string]string{
		"pg_prove_notests.txt":     "no pgTAP test ran",
		"pg_prove_result_fail.txt": "did not report PASS",
		"pg_prove_no_summary.txt":  "no harness summary",
	} {
		e := setup(t, pgCfg, "pg_prove")
		t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
		fakeOut(t, name)
		t.Setenv("TQ_FAKE_EXIT", "0")
		t.Setenv("TEST_DATABASE_URL", "postgres://tester@localhost:54322/postgres")
		dialer(t, e, "up")
		r := Run(context.Background(), e.in, opts(e, PgProve))
		if r.Status != packs.Fail || !strings.Contains(r.Detail, want) || len(r.Findings) != 1 || r.Findings[0].Rule != "pg_prove:run-failed" {
			t.Errorf("%s: result = %+v", name, r)
		}
	}
	// Supabase mode reads the same harness summary.
	e := setup(t, "db:\n  tools: [supabase]\n", "supabase")
	t.Setenv("TQ_FAKE_PGTAP", "supabase")
	fakeOut(t, "pg_prove_notests.txt")
	dialer(t, e, "up")
	if r := Run(context.Background(), e.in, opts(e, Supabase)); r.Status != packs.Fail || !strings.Contains(r.Detail, "no pgTAP test ran") {
		t.Errorf("supabase NOTESTS: %+v", r)
	}
}

// The tests directory is never read as a pg_prove option: Run refuses one
// that starts with "-" (or escapes), and passes "--" before it.
func TestTestsDirIsNeverAnOption(t *testing.T) {
	for _, dir := range []string{"--psql-bin=./evil", "-x", "tests/-x", "tests/../..", "/abs"} {
		e := setup(t, pgCfg, "pg_prove")
		t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
		t.Setenv("TEST_DATABASE_URL", "postgres://tester@localhost:54322/postgres")
		dialer(t, e, "up")
		o := opts(e, PgProve)
		o.TestsDir = dir
		if r := Run(context.Background(), e.in, o); r.Status != packs.Fail || !strings.Contains(r.Detail, "packs.db.rls.tests") {
			t.Errorf("%q: result = %+v", dir, r)
		}
		if _, err := os.Stat(e.argv); err == nil {
			t.Errorf("%q: pg_prove ran", dir)
		}
	}
}

// A password too short for the old replacement threshold is still masked.
func TestShortPasswordIsMasked(t *testing.T) {
	e := setup(t, pgCfg, "pg_prove")
	t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
	t.Setenv("TQ_FAKE_OUT", "")
	t.Setenv("TQ_FAKE_EXIT", "2")
	t.Setenv("TQ_FAKE_ECHO_SECRET", "1")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester:qz@localhost/acme") // tq:allow-secret fake test fixture
	dialer(t, e, "up")
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.Fail || strings.Contains(r.Detail, "qz") || !strings.Contains(r.Detail, "[REDACTED:test_db]") {
		t.Fatalf("result = %+v", r)
	}
}

// The run-failed tail drops the SQL that psql echoes from the test files.
func TestTailDropsEchoedSQL(t *testing.T) {
	e := setup(t, pgCfg, "pg_prove")
	t.Setenv("TQ_FAKE_PGTAP", "pg_prove")
	fakeOut(t, "pg_prove_sql_error.txt")
	t.Setenv("TQ_FAKE_EXIT", "3")
	t.Setenv("TEST_DATABASE_URL", "postgres://tester@localhost/acme")
	dialer(t, e, "up")
	r := Run(context.Background(), e.in, opts(e, PgProve))
	if r.Status != packs.Fail {
		t.Fatalf("result = %+v", r)
	}
	for _, leak := range []string{"LINE 1:", "QUERY:", "CONTEXT:", "owner_id = 7"} {
		if strings.Contains(r.Detail, leak) {
			t.Errorf("detail keeps %q:\n%s", leak, r.Detail)
		}
	}
	if !strings.Contains(r.Detail, "Dubious, test returned 3") {
		t.Errorf("detail lost the harness line:\n%s", r.Detail)
	}
}

// writeDockerContext writes a Docker CLI config whose current context is
// name, with an endpoint host when host is set.
func writeDockerContext(t *testing.T, dir, name, host string) {
	t.Helper()
	writeFile(t, dir, "config.json", `{"auths":{},"currentContext":"`+name+`"}`)
	if host != "" {
		writeFile(t, dir, "contexts/meta/"+contextDigest(name)+"/meta.json",
			`{"Name":"`+name+`","Metadata":{},"Endpoints":{"docker":{"Host":"`+host+`","SkipTLSVerify":false}}}`)
	}
}

// The current Docker context can point the supabase CLI at another machine
// as surely as DOCKER_HOST: a remote one is refused before anything
// connects, a local one runs.
func TestSupabaseDockerContextMustBeLocal(t *testing.T) {
	cases := []struct {
		name, host string
		ok         bool
	}{
		{"build", "ssh://tester@build.acme.example", false},
		{"remote", "tcp://10.0.0.5:2376", false},
		{"missing", "", false},
		{"colima", "unix:///home/tester/.colima/default/docker.sock", true},
		{"default", "", true},
	}
	for _, c := range cases {
		e := setup(t, "db:\n  tools: [supabase]\n", "supabase")
		t.Setenv("TQ_FAKE_PGTAP", "supabase")
		home, _ := os.UserHomeDir()
		writeDockerContext(t, filepath.Join(home, ".docker"), c.name, c.host)
		dialed := dialer(t, e, "up")
		r := Run(context.Background(), e.in, opts(e, Supabase))
		if c.ok {
			if r.Status != packs.OK {
				t.Errorf("%s: %+v", c.name, r)
			}
			continue
		}
		if r.Status != packs.Fail || !strings.Contains(r.Detail, "not local") || strings.Contains(r.Detail, "acme.example") {
			t.Errorf("%s: %+v", c.name, r)
		}
		if len(*dialed) != 0 {
			t.Errorf("%s: dialed %v", c.name, *dialed)
		}
	}
	// DOCKER_CONFIG names the config directory instead of ~/.docker.
	e := setup(t, "db:\n  tools: [supabase]\n", "supabase")
	t.Setenv("TQ_FAKE_PGTAP", "supabase")
	cfgDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", cfgDir)
	writeDockerContext(t, cfgDir, "build", "tcp://build.acme.example:2376")
	dialer(t, e, "up")
	if r := Run(context.Background(), e.in, opts(e, Supabase)); r.Status != packs.Fail || !strings.Contains(r.Detail, "not local") {
		t.Errorf("DOCKER_CONFIG: %+v", r)
	}
	// DOCKER_CONTEXT in the parent picks the context too.
	t.Setenv("DOCKER_CONFIG", "")
	e = setup(t, "db:\n  tools: [supabase]\n", "supabase")
	t.Setenv("TQ_FAKE_PGTAP", "supabase")
	home, _ := os.UserHomeDir()
	writeDockerContext(t, filepath.Join(home, ".docker"), "default", "")
	writeFile(t, filepath.Join(home, ".docker"), "contexts/meta/"+contextDigest("build")+"/meta.json",
		`{"Name":"build","Endpoints":{"docker":{"Host":"tcp://build.acme.example:2376"}}}`)
	t.Setenv("DOCKER_CONTEXT", "build")
	dialer(t, e, "up")
	if r := Run(context.Background(), e.in, opts(e, Supabase)); r.Status != packs.Fail || !strings.Contains(r.Detail, "not local") {
		t.Errorf("DOCKER_CONTEXT: %+v", r)
	}
}

func TestParseTAPSummary(t *testing.T) {
	r := ParseTAP([]byte("Files=0, Tests=0,  0 wallclock secs\nResult: NOTESTS\n"), nil)
	if !r.Summary || r.Pass || r.Files != 0 || r.Tests != 0 || r.Result != "NOTESTS" {
		t.Fatalf("NOTESTS = %+v", r)
	}
	r = ParseTAP([]byte("a.sql .. ok\nAll tests successful.\n"), nil)
	if r.Summary || r.Pass || r.Result != "" {
		t.Fatalf("no summary = %+v", r)
	}
	r = ParseTAP([]byte("Files=2, Tests=12,  0 wallclock secs\nResult: PASS\n"), nil)
	if !r.Summary || !r.Pass || r.Tests != 12 {
		t.Fatalf("PASS = %+v", r)
	}
}
