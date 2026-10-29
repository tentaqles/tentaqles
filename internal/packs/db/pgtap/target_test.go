package pgtap

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in    string
		hosts []string
		db    string
		user  string
		pass  bool
	}{
		{"postgres://tester:s3cret-pw@localhost:54322/postgres", []string{"localhost:54322"}, "postgres", "tester", true}, // tq:allow-secret fake test fixture
		{"postgresql://tester@127.0.0.1/acme?sslmode=disable", []string{"127.0.0.1:5432"}, "acme", "tester", false},
		{"postgres://[::1]:6543/acme", []string{"::1:6543"}, "acme", "", false},
		{"postgresql://%2Fvar%2Frun%2Fpostgresql/acme", []string{"/var/run/postgresql:5432"}, "acme", "", false},
		{"postgresql:///acme?host=/tmp&port=5433", []string{"/tmp:5433"}, "acme", "", false},
		{"postgres://localhost:5432,db:5433/acme", []string{"localhost:5432", "db:5433"}, "acme", "", false},
		{"host=localhost port=5433 dbname=acme user=tester password='a b'", []string{"localhost:5433"}, "acme", "tester", true},
		{"postgres://tester:p%40ss@db/acme", []string{"db:5432"}, "acme", "tester", true}, // tq:allow-secret fake test fixture
	}
	for _, c := range cases {
		tg, err := ParseTarget(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		var hs []string
		for _, h := range tg.Hosts {
			hs = append(hs, h.String())
		}
		if !reflect.DeepEqual(hs, c.hosts) || tg.DB != c.db || tg.User != c.user || (tg.Password != "") != c.pass {
			t.Errorf("%s: got %+v", c.in, tg)
		}
	}
	tg, _ := ParseTarget("postgres://tester:p%40ss@db/acme") // tq:allow-secret fake test fixture
	if tg.Password != "p@ss" {
		t.Errorf("percent-decoding: %q", tg.Password)
	}
	for _, bad := range []string{"", "mysql://localhost/x", "jdbc:postgresql://localhost/x", "postgres://localhost:notaport/x", "host=localhost service=prod"} {
		if _, err := ParseTarget(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	// An error never quotes the value.
	_, err := ParseTarget("postgres://tester:s3cret-pw@localhost:bad/x") // tq:allow-secret fake test fixture
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("err = %v", err)
	}
}

func TestLocal(t *testing.T) {
	local := []string{
		"postgres://localhost/x", "postgres://127.0.0.1/x", "postgres://[::1]/x", "postgres://127.0.0.2/x",
		"postgresql://%2Ftmp/x", "postgres://db/x", "host=localhost,db dbname=x",
	}
	for _, s := range local {
		tg, err := ParseTarget(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := tg.Local([]string{"db"}); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	remote := []string{
		"postgres://db.abcdefghijklmnop.supabase.co/postgres", "postgres://10.0.0.5/x", "postgres://localhost,10.0.0.5/x",
		"postgres://db/x?hostaddr=10.0.0.5", "postgres://acme-sql.example.com/x", "postgres://localhost.example.com/x",
	}
	for _, s := range remote {
		tg, err := ParseTarget(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := tg.Local([]string{"db"}); err == nil {
			t.Errorf("%s counted as local", s)
		}
	}
	// A compose service name is local only when listed.
	tg, _ := ParseTarget("postgres://db/x")
	if tg.Local(nil) == nil {
		t.Error("unlisted service name counted as local")
	}
}

func TestEnvAndArgs(t *testing.T) {
	tg, _ := ParseTarget("postgres://tester:s3cret-pw@localhost:54322/acme?sslmode=disable") // tq:allow-secret fake test fixture
	env := tg.Env()
	want := []string{"PGHOST=localhost", "PGPORT=54322", "PGDATABASE=acme", "PGUSER=tester", "PGPASSWORD=s3cret-pw", "PGSSLMODE=disable"}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("env = %q", env)
	}
	args := tg.Args()
	if !reflect.DeepEqual(args, []string{"--host", "localhost", "--port", "54322", "--dbname", "acme", "--username", "tester"}) {
		t.Fatalf("args = %q", args)
	}
	for _, a := range args {
		if strings.Contains(a, "s3cret") {
			t.Fatal("password on argv")
		}
	}
}
