package rls

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Each fixture X.sql states its expected findings on its first line,
// "-- want: RULE:level:line ... [| opts: k=v ...]". X.before.sql, when
// present, is replayed first and not judged. The exposed schema is public
// (plus storage with supabase=1).
func TestGoldenFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		if strings.HasSuffix(f, ".before.sql") {
			continue
		}
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			want, opts := parseWant(string(src))
			o := Options{Exposed: []string{"public"}}
			migrator := "raw"
			if opts["supabase"] == "1" {
				o.Supabase = true
				o.Exposed = append(o.Exposed, "storage")
			}
			if m := opts["migrator"]; m != "" {
				migrator = m
			}
			var units []Unit
			if b, err := os.ReadFile(strings.TrimSuffix(f, ".sql") + ".before.sql"); err == nil {
				units = append(units, Unit{Path: "m/0000_before.sql", Src: b, Migrator: migrator})
			}
			name := "m/0001_" + filepath.Base(f)
			units = append(units, Unit{Path: name, Src: src, Judge: true, Migrator: migrator})
			res := Run(units, o)
			var got []string
			for _, fd := range res.Findings {
				if fd.File != name {
					t.Errorf("finding on %s: %v", fd.File, fd)
				}
				if fd.Message == "" {
					t.Errorf("finding %s has no message", fd.Rule)
				}
				got = append(got, fmt.Sprintf("%s:%s:%d", fd.Rule, fd.Level, fd.Line))
			}
			sort.Strings(got)
			if len(got) != len(want) || (len(got) > 0 && !reflect.DeepEqual(got, want)) {
				t.Fatalf("findings = %v, want %v\n%v", got, want, res.Findings)
			}
		})
	}
}

func parseWant(src string) ([]string, map[string]string) {
	first, _, _ := strings.Cut(src, "\n")
	first = strings.TrimPrefix(strings.TrimSpace(first), "-- want:")
	spec, optStr, _ := strings.Cut(first, "| opts:")
	opts := map[string]string{}
	for _, kv := range strings.Fields(optStr) {
		k, v, _ := strings.Cut(kv, "=")
		opts[k] = v
	}
	want := strings.Fields(spec)
	sort.Strings(want)
	return want, opts
}

// Every rule has a positive and a negative fixture.
func TestEveryRuleHasFixtures(t *testing.T) {
	for id := range Rules() {
		for _, suffix := range []string{".sql", "_ok.sql"} {
			if _, err := os.Stat(filepath.Join("testdata", id+suffix)); err != nil {
				t.Errorf("%s has no %s fixture", id, suffix)
			}
		}
	}
}

func TestSuppressionsAndTables(t *testing.T) {
	src := "create table public.notes (id bigint);\n-- tq:allow RLS001: filled by a trigger, never served\ncreate table public.raw (id bigint);\nalter table public.notes enable row level security;\ncreate policy p on public.notes for select to authenticated using (id > 0);\n"
	res := Run([]Unit{{Path: "a.sql", Src: []byte(src), Judge: true}}, Options{Exposed: []string{"public"}})
	if len(res.Findings) != 0 || !reflect.DeepEqual(res.Suppressed, []string{"RLS001"}) {
		t.Fatalf("findings %v suppressed %v", res.Findings, res.Suppressed)
	}
	var names []string
	for _, tb := range res.Tables {
		names = append(names, fmt.Sprintf("%s rls=%v", tb.Name, tb.RLS))
	}
	if !reflect.DeepEqual(names, []string{"public.notes rls=true", "public.raw rls=false"}) {
		t.Fatalf("tables = %v", names)
	}
}

// Replay-only files are never reported, even when they break a rule.
func TestReplayOnlyIsNotJudged(t *testing.T) {
	old := "create table public.notes (id bigint);\ngrant all on public.notes to anon;\ncreate view public.v as select 1;\n"
	res := Run([]Unit{{Path: "old.sql", Src: []byte(old)}, {Path: "new.sql", Src: []byte("select 1;\n"), Judge: true}}, Options{Exposed: []string{"public"}})
	if len(res.Findings) != 0 {
		t.Fatalf("findings on replay-only file: %v", res.Findings)
	}
}
