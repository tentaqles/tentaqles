package drill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// Fake CLIs (spec D §21): copies of this test binary named psql,
// pg_restore, supabase, sqlcmd and sqlpackage. Each records its argv and
// the names (never the values) of a few variables, and prints fixture
// output steered by TQ_FAKE_* variables.

var fakeTools = []string{"psql", "pg_restore", "supabase", "sqlcmd", "sqlpackage", "fetchdump"}

func init() {
	toolrun.PassForTests("TQ_FAKE_DRILL", "TQ_FAKE_DRILL_LOG", "TQ_FAKE_DRILL_COUNT", "TQ_FAKE_DRILL_FAIL", "TQ_FAKE_DRILL_FIX", "TQ_FAKE_DRILL_VERSION")
}

func TestMain(m *testing.M) {
	if os.Getenv("TQ_FAKE_DRILL") == "1" || isFake(os.Args[0]) {
		os.Exit(fakeMain())
	}
	// A fresh copy of this binary can start slower than the 5 s --version
	// bound on a loaded Windows runner; the probe would record no version.
	toolrun.GenerousVersionTimeoutForTests()
	os.Exit(m.Run())
}

func baseName(p string) string {
	b := strings.ToLower(filepath.Base(p))
	return strings.TrimSuffix(b, ".exe")
}

func isFake(arg0 string) bool {
	n := baseName(arg0)
	for _, t := range fakeTools {
		if n == t {
			return true
		}
	}
	return false
}

type fakeCall struct {
	Tool string   `json:"tool"`
	Args []string `json:"args"`
	// Env lists which connection variables were set (names only).
	Env []string `json:"env"`
	// PW is true when the password reached the tool through its variable.
	PW bool `json:"pw"`
}

func fakeMain() int {
	tool := baseName(os.Args[0])
	args := os.Args[1:]
	if lf := os.Getenv("TQ_FAKE_DRILL_LOG"); lf != "" {
		c := fakeCall{Tool: tool, Args: args}
		for _, k := range []string{"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "SQLCMDPASSWORD", "TQ_DRILL_OUT"} {
			if os.Getenv(k) != "" {
				c.Env = append(c.Env, k)
			}
		}
		c.PW = os.Getenv("PGPASSWORD") == "s3cret-tester" || os.Getenv("SQLCMDPASSWORD") == "s3cret-tester" // tq:allow-secret fake test fixture
		f, _ := os.OpenFile(lf, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		json.NewEncoder(f).Encode(c)
		f.Close()
	}
	if len(args) > 0 && args[0] == "--version" {
		os.Stdout.WriteString(tool + " 16.2\n")
		return 0
	}
	fail := os.Getenv("TQ_FAKE_DRILL_FAIL")
	joined := strings.Join(args, " ")
	if fail != "" && strings.Contains(joined, fail) {
		os.Stderr.WriteString("ERROR: fake failure for row tester@acme.test\n")
		return 1
	}
	switch tool {
	case "fetchdump":
		out := os.Getenv("TQ_DRILL_OUT")
		b, _ := os.ReadFile(filepath.Join(os.Getenv("TQ_FAKE_DRILL_FIX"), "plain.sql"))
		os.WriteFile(out, b, 0o600)
		return 0
	case "supabase":
		for i, a := range args {
			if a == "-f" && i+1 < len(args) {
				os.WriteFile(args[i+1], []byte("-- PostgreSQL database dump\n"), 0o644)
			}
		}
		return 0
	case "pg_restore", "sqlpackage":
		return 0
	case "psql":
		q := ""
		for i, a := range args {
			if a == "-c" && i+1 < len(args) {
				q = args[i+1]
			}
		}
		return fakeAnswer(q)
	case "sqlcmd":
		q := ""
		for i, a := range args {
			if a == "-Q" && i+1 < len(args) {
				q = args[i+1]
			}
		}
		switch {
		case strings.Contains(q, "HEADERONLY"):
			cols := make([]string, 30)
			cols[18] = "2026-10-20 03:00:00.000"
			os.Stdout.WriteString(strings.Join(cols, "|") + "\n")
			return 0
		case strings.Contains(q, "FILELISTONLY"):
			os.Stdout.WriteString("acme|/var/opt/mssql/data/acme.mdf|D\nacme_log|/var/opt/mssql/data/acme_log.ldf|L\n")
			return 0
		case strings.Contains(q, "SERVERPROPERTY"):
			os.Stdout.WriteString("/var/opt/mssql/data/|/var/opt/mssql/log/\n")
			return 0
		}
		return fakeAnswer(q)
	}
	return 3
}

func fakeAnswer(q string) int {
	l := strings.ToLower(q)
	switch {
	case q == "":
		return 0
	case strings.Contains(l, "pg_tables") || strings.Contains(l, "sys.tables where"):
		os.Stdout.WriteString("12\n")
	case strings.Contains(l, "pg_indexes") || strings.Contains(l, "sys.indexes"):
		os.Stdout.WriteString("30\n")
	case strings.Contains(l, "count(*)") || strings.Contains(l, "count_big(*)"):
		n := os.Getenv("TQ_FAKE_DRILL_COUNT")
		if n == "" {
			n = "42"
		}
		os.Stdout.WriteString(n + "\n")
	case strings.Contains(l, "schema_migrations") || strings.Contains(l, "efmigrationshistory"):
		v := os.Getenv("TQ_FAKE_DRILL_VERSION")
		if v == "" {
			v = "20261001000000"
		}
		os.Stdout.WriteString(v + "\n")
	}
	return 0
}

// installFakes puts the fake tools first on PATH.
func installFakes(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	for _, tool := range fakeTools {
		if err := os.WriteFile(filepath.Join(bin, tool+exe), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

func readCalls(t *testing.T, log string) []fakeCall {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	var out []fakeCall
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		var c fakeCall
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}
