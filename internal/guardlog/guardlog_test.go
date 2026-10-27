package guardlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendMasksTruncatesAndReads(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	long := "psql -c \"delete from t where email='tester@example.com'\" " + strings.Repeat("x", 400)
	Append(Record{WS: "acme", Scope: "demo-todo", Mode: "auto", Rules: []string{"tq/sql-destructive-shell"}, Tool: "Bash", Command: long}, []string{"pii"})
	Append(Record{WS: "other", Command: "ls"}, nil)
	recs := Read("acme", time.Now().Add(-time.Hour), 0)
	if len(recs) != 1 {
		t.Fatalf("want 1 acme record, got %d", len(recs))
	}
	c := recs[0].Command
	if strings.Contains(c, "tester@example.com") {
		t.Fatalf("email not masked: %q", c)
	}
	if n := len([]rune(c)); n > 200 {
		t.Fatalf("command not truncated: %d runes", n)
	}
	if recs[0].Time.IsZero() {
		t.Fatal("time not set")
	}
}

func TestAppendRotates(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), make([]byte, rotateAt), 0o600); err != nil {
		t.Fatal(err)
	}
	Append(Record{WS: "acme", Command: "ls"}, nil)
	if _, err := os.Stat(Path() + ".1"); err != nil {
		t.Fatalf("no rotated file: %v", err)
	}
	if fi, _ := os.Stat(Path()); fi.Size() > 1024 {
		t.Fatalf("new file not fresh: %d bytes", fi.Size())
	}
}

func fileLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitAfter(strings.TrimSuffix(string(b), "\n"), "\n")
}

func TestReadTailDropsPartialFirstLine(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	for _, c := range []string{"first", "second", "third"} {
		Append(Record{WS: "acme", Command: c}, nil)
	}
	lines := fileLines(t, Path())
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	// The tail starts 5 bytes inside the first line: that partial line is
	// dropped, the last two come back newest first.
	tail := int64(len(lines[1]) + len(lines[2]) + 1 + 5)
	recs := Read("acme", time.Time{}, tail)
	if len(recs) != 2 || recs[0].Command != "third" || recs[1].Command != "second" {
		t.Fatalf("tail read = %+v", recs)
	}
	if all := Read("acme", time.Time{}, 0); len(all) != 3 {
		t.Fatalf("maxBytes 0 must read the whole file, got %d", len(all))
	}
}

func TestReadAllIncludesRotatedFile(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	Append(Record{WS: "acme", Command: "old"}, nil)
	if err := os.Rename(Path(), Path()+".1"); err != nil {
		t.Fatal(err)
	}
	Append(Record{WS: "acme", Command: "new"}, nil)
	Append(Record{WS: "other", Command: "elsewhere"}, nil)
	recs := ReadAll("acme", time.Time{})
	if len(recs) != 2 || recs[0].Command != "new" || recs[1].Command != "old" {
		t.Fatalf("ReadAll = %+v", recs)
	}
	if cur := Read("acme", time.Time{}, 0); len(cur) != 1 {
		t.Fatalf("Read stays on the current file, got %+v", cur)
	}
}
