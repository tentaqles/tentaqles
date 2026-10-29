package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/db/sqllex"
)

// Each fixture X.sql states its expected findings on its first line,
// "-- want: RULE:level:line ...", optionally "| opts: k=v ..." (also on an
// "-- opts:" line). X.before.sql, when present, is replayed first as an
// already-applied migration.
func TestGoldenFixtures(t *testing.T) {
	for _, dir := range []string{"pg", "ms"} {
		d := sqllex.Postgres
		if dir == "ms" {
			d = sqllex.TSQL
		}
		files, err := filepath.Glob(filepath.Join("testdata", dir, "*.sql"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no fixtures in %s", dir)
		}
		for _, f := range files {
			if strings.HasSuffix(f, ".before.sql") {
				continue
			}
			f := f
			t.Run(dir+"/"+filepath.Base(f), func(t *testing.T) {
				src, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				want, opts := parseWant(t, string(src))
				o := Options{Dialect: d, Migrator: "raw"}
				for k, v := range opts {
					switch k {
					case "small":
						o.SmallTables = strings.Split(v, ",")
					case "edition":
						o.Edition = v
					case "migrator":
						o.Migrator = v
					}
				}
				var units []Unit
				before := strings.TrimSuffix(f, ".sql") + ".before.sql"
				if b, err := os.ReadFile(before); err == nil {
					units = append(units, Unit{Path: "m/0000_before.sql", Src: b})
				}
				units = append(units, Unit{Path: "m/0001_" + filepath.Base(f), Src: src, New: true, Lint: true})
				res := Run(units, o)
				var got []string
				for _, fd := range res.Findings {
					if fd.File != "m/0001_"+filepath.Base(f) {
						t.Errorf("finding on %s", fd.File)
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
}

func parseWant(t *testing.T, src string) ([]string, map[string]string) {
	t.Helper()
	first, _, _ := strings.Cut(src, "\n")
	opts := map[string]string{}
	head := strings.TrimSpace(strings.TrimPrefix(first, "-- want:"))
	if a, b, ok := strings.Cut(head, "|"); ok {
		head = a
		parseOpts(strings.TrimPrefix(strings.TrimSpace(b), "opts:"), opts)
	}
	for _, l := range strings.Split(src, "\n") {
		if strings.HasPrefix(l, "-- opts:") {
			parseOpts(strings.TrimPrefix(l, "-- opts:"), opts)
		}
	}
	if !strings.HasPrefix(first, "-- want:") {
		// The header line may be second (a header directive comes first).
		for _, l := range strings.Split(src, "\n") {
			if strings.HasPrefix(l, "-- want:") {
				head = strings.TrimSpace(strings.TrimPrefix(l, "-- want:"))
			}
		}
	}
	want := strings.Fields(head)
	sort.Strings(want)
	if want == nil {
		want = nil
	}
	return want, opts
}

func parseOpts(s string, into map[string]string) {
	for _, kv := range strings.Fields(s) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			into[k] = v
		}
	}
}

func TestEveryRuleHasPositiveAndNegativeFixture(t *testing.T) {
	for _, r := range Rules() {
		dir := "pg"
		if strings.HasPrefix(r.ID, "MS") {
			dir = "ms"
		}
		for _, suffix := range []string{".sql", "_ok.sql"} {
			if _, err := os.Stat(filepath.Join("testdata", dir, r.ID+suffix)); err != nil {
				t.Errorf("rule %s: missing fixture %s%s", r.ID, r.ID, suffix)
			}
		}
		if r.Fix == "" || r.Title == "" {
			t.Errorf("rule %s has no title or fix", r.ID)
		}
	}
}

func TestEarlierNewMigrationMakesTableNew(t *testing.T) {
	units := []Unit{
		{Path: "m/0001_create.sql", Src: []byte("CREATE TABLE acme_orders (id int);"), New: true, Lint: true},
		{Path: "m/0002_index.sql", Src: []byte("CREATE INDEX i ON acme_orders (id);"), New: true, Lint: true},
	}
	if res := Run(units, Options{Dialect: sqllex.Postgres}); len(res.Findings) != 0 {
		t.Fatalf("a table created by an unapplied migration in this diff is new: %v", res.Findings)
	}
	units[0].New, units[0].Lint = false, false
	res := Run(units, Options{Dialect: sqllex.Postgres})
	if len(res.Findings) == 0 || res.Findings[0].Rule != "PG001" {
		t.Fatalf("an applied migration's table is existing: %v", res.Findings)
	}
}

func TestSuppressedRulesReported(t *testing.T) {
	src := "SET lock_timeout = '1s';\n-- tq:allow PG001: tiny lookup table\nCREATE INDEX i ON orders (a);"
	res := Run([]Unit{{Path: "a.sql", Src: []byte(src), New: true, Lint: true}}, Options{Dialect: sqllex.Postgres})
	if len(res.Findings) != 0 || !reflect.DeepEqual(res.Suppressed, []string{"PG001"}) {
		t.Fatalf("findings %v suppressed %v", res.Findings, res.Suppressed)
	}
}

func TestMigratorTransactions(t *testing.T) {
	cic := "CREATE INDEX CONCURRENTLY i ON orders (a);"
	cases := []struct {
		migrator, src string
		noTx          bool
		want          bool
	}{
		{"raw", cic, false, false},
		{"dbmate", "-- migrate:up\n" + cic + "\n-- migrate:down\nDROP INDEX CONCURRENTLY i;", false, true},
		{"dbmate", "-- migrate:up transaction:false\n" + cic, false, false},
		{"supabase", cic, false, true},
		{"drizzle", cic, false, true},
		{"flyway", cic, false, true},
		{"flyway", cic, true, false},
		{"golang-migrate", cic, false, false},
		{"golang-migrate", "SET lock_timeout = '1s';\n" + cic, false, true},
		{"sqitch", "BEGIN;\n" + cic + "\nCOMMIT;", false, true},
	}
	for _, c := range cases {
		res := Run([]Unit{{Path: "a.sql", Src: []byte(c.src), New: true, Lint: true, NoTransaction: c.noTx}}, Options{Dialect: sqllex.Postgres, Migrator: c.migrator})
		got := false
		for _, f := range res.Findings {
			if f.Rule == "PG010" {
				got = true
			}
		}
		if got != c.want {
			t.Errorf("%s noTx=%v: PG010 = %v, want %v (%v)", c.migrator, c.noTx, got, c.want, res.Findings)
		}
	}
}

func TestMessagesNeverQuoteLiterals(t *testing.T) {
	src := "UPDATE orders SET email = 'tester@acme.test';\nALTER TABLE orders ADD COLUMN token text NOT NULL;"
	res := Run([]Unit{{Path: "a.sql", Src: []byte(src), New: true, Lint: true}}, Options{Dialect: sqllex.Postgres})
	for _, f := range res.Findings {
		if strings.Contains(f.Message, "tester@acme.test") {
			t.Fatalf("message quotes a literal: %s", f.Message)
		}
	}
}
