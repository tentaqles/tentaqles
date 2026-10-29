package rls

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseSupabaseConfig(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "config", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	c := ParseSupabaseConfig(src)
	if !c.HasSchemas || !reflect.DeepEqual(c.Schemas, []string{"public", "graphql_public", "api"}) {
		t.Fatalf("schemas = %v (%v)", c.Schemas, c.HasSchemas)
	}
	if c.DBPort != 54322 {
		t.Fatalf("db port = %d", c.DBPort)
	}
	// Single-line, single quotes, a trailing comment, and no [api] at all.
	c = ParseSupabaseConfig([]byte("[api]\nenabled = true\nschemas = ['public', \"acme\"] # exposed\nextra_search_path = [\"public\", \"extensions\"]\n[db]\nport = 6543\n"))
	if !reflect.DeepEqual(c.Schemas, []string{"public", "acme"}) || c.DBPort != 6543 {
		t.Fatalf("got %+v", c)
	}
	c = ParseSupabaseConfig([]byte("project_id = \"acme\"\n[db]\nmajor_version = 15\n"))
	if c.HasSchemas || c.DBPort != 0 {
		t.Fatalf("got %+v", c)
	}
	// A key named schemas in another table is not [api] schemas.
	c = ParseSupabaseConfig([]byte("[storage]\nschemas = [\"x\"]\n[api]\nport = 54321\n"))
	if c.HasSchemas {
		t.Fatalf("schemas from [storage]: %+v", c)
	}
	// Dotted key form at the top level.
	c = ParseSupabaseConfig([]byte("api.schemas = [\"public\", \"acme\"]\n"))
	if !reflect.DeepEqual(c.Schemas, []string{"public", "acme"}) {
		t.Fatalf("dotted: %+v", c)
	}
}

func TestExposedSchemas(t *testing.T) {
	cases := []struct {
		name     string
		cfg      []string
		toml     string
		supabase bool
		want     []string
	}{
		{"explicit wins", []string{"public", "acme"}, "[api]\nschemas = [\"other\"]\n", true, []string{"acme", "public"}},
		{"plain postgres default", nil, "", false, []string{"public"}},
		{"supabase config, public and storage always included", nil, "[api]\nschemas = [\"api\"]\n", true, []string{"api", "public", "storage"}},
		{"supabase without the key", nil, "[api]\nport = 1\n", true, []string{"graphql_public", "public", "storage"}},
	}
	for _, c := range cases {
		var tm *SupabaseConfig
		if c.toml != "" {
			p := ParseSupabaseConfig([]byte(c.toml))
			tm = &p
		}
		if got := ExposedSchemas(c.cfg, tm, c.supabase); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func node(t *testing.T, s string) yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(s), &n); err != nil {
		t.Fatal(err)
	}
	return *n.Content[0]
}

func TestValidateRLS(t *testing.T) {
	if err := ValidateRLS(node(t, "{exposed_schemas: [public, api], tests: supabase/tests/database}")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"[public]",
		"{exposed: [public]}",
		"{exposed_schemas: public}",
		"{exposed_schemas: ['']}",
		"{tests: /abs}",
		"{tests: ../out}",
		"{tests: [a]}",
	} {
		if err := ValidateRLS(node(t, bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestUntested(t *testing.T) {
	src := "create table public.notes (id bigint);\nalter table public.notes enable row level security;\ncreate table public.note_tags (id bigint);\nalter table public.note_tags enable row level security;\ncreate table public.open (id bigint);\n"
	res := Run([]Unit{{Path: "a.sql", Src: []byte(src)}}, Options{Exposed: []string{"public"}})
	tests := map[string][]byte{"t/rls_notes.test.sql": []byte("select results_eq('select count(*)::int from public.notes', array[1]);")}
	var got []string
	for _, tb := range Untested(res.Tables, tests) {
		got = append(got, tb.Name.String())
	}
	// public.notes is named; note_tags is not (notes is a different word);
	// public.open has no RLS, so RLS001 is its problem, not coverage.
	if !reflect.DeepEqual(got, []string{"public.note_tags"}) {
		t.Fatalf("untested = %v", got)
	}
	tests["t/tags.sql"] = []byte(`select ok(true, '"note_tags" checked');`)
	if got := Untested(res.Tables, tests); len(got) != 0 {
		t.Fatalf("untested = %v", got)
	}
}

func TestCoverage(t *testing.T) {
	src := "create table public.notes (id bigint);\nalter table public.notes enable row level security;\n-- tq:allow RLST001: covered by the e2e suite\ncreate table public.tags (id bigint);\nalter table public.tags enable row level security;\nalter table storage.objects enable row level security;\n"
	res := Run([]Unit{{Path: "m/1.sql", Src: []byte(src)}}, Options{Exposed: []string{"public", "storage"}})
	fs, supp, names := Coverage(res.Tables, map[string][]byte{}, "supabase/tests", "/tentaqles:rls-tests")
	var got []string
	for _, f := range fs {
		got = append(got, f.String())
	}
	want := []string{
		"m/1.sql:1 error RLST001 table public.notes has RLS and no pgTAP test under supabase/tests that names it; write one with /tentaqles:rls-tests public.notes",
		"m/1.sql:6 error RLST001 table storage.objects has RLS and no pgTAP test under supabase/tests that names it; write one with /tentaqles:rls-tests storage.objects",
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(supp, []string{"RLST001"}) || !reflect.DeepEqual(names, []string{"public.notes", "storage.objects"}) {
		t.Fatalf("got %q suppressed %v", got, supp)
	}
}
