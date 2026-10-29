package pgtap

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func golden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func failures(fs []Failure) []string {
	var out []string
	for _, f := range fs {
		out = append(out, fmt.Sprintf("%s:%d %s", f.File, f.Test, f.Desc))
	}
	return out
}

func TestParseFailures(t *testing.T) {
	res := ParseTAP(golden(t, "pg_prove_fail.txt"), nil)
	want := []string{
		"supabase/tests/database/rls_more.test.sql:3 ",
		"supabase/tests/database/rls_more.test.sql:7 ",
		"supabase/tests/database/rls_more.test.sql:8 ",
		"supabase/tests/database/rls_notes.test.sql:2 a cannot write as b",
		"supabase/tests/database/rls_notes.test.sql:5 service sees all",
		"supabase/tests/database/rls_users.test.sql:0 Bad plan.  You planned 4 tests but ran 1.",
	}
	if got := failures(res.Failures); !reflect.DeepEqual(got, want) {
		t.Fatalf("failures =\n%q\nwant\n%q", got, want)
	}
	if res.Files != 4 || res.Tests != 20 || res.Pass {
		t.Fatalf("summary = %+v", res)
	}
}

func TestParsePass(t *testing.T) {
	res := ParseTAP(golden(t, "pg_prove_ok.txt"), nil)
	if len(res.Failures) != 0 || res.Files != 2 || res.Tests != 12 || !res.Pass {
		t.Fatalf("got %+v", res)
	}
}

// supabase test db runs pg_prove in a container: its paths are mapped back
// to the repository's test files.
func TestParseSupabaseMapsPaths(t *testing.T) {
	local := []string{"supabase/tests/database/rls_notes.test.sql", "supabase/tests/database/rls_tags.test.sql"}
	res := ParseTAP(golden(t, "supabase_fail.txt"), MapPaths(local, "supabase/tests"))
	if got := failures(res.Failures); !reflect.DeepEqual(got, []string{"supabase/tests/database/rls_notes.test.sql:3 a cannot update b"}) {
		t.Fatalf("failures = %q", got)
	}
}

func TestMapPaths(t *testing.T) {
	m := MapPaths([]string{"db/tests/a.sql", "db/tests/x/b.sql"}, "db/tests")
	for in, want := range map[string]string{
		"db/tests/a.sql":              "db/tests/a.sql",
		"/tmp/supabase/tests/x/b.sql": "db/tests/x/b.sql",
		"/somewhere/a.sql":            "db/tests/a.sql",
		"/tmp/unknown.sql":            "db/tests/unknown.sql",
	} {
		if got := m(in); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}
