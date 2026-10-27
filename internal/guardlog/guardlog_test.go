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
