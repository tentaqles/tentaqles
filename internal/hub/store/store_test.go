package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// isolate points TQ_HOME, HOME and USERPROFILE at a temp dir: no test touches the real home.
func isolate(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func mustOpen(t *testing.T, ws string) *Store {
	t.Helper()
	s, err := Open(ws, []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesLayoutAndMigrates(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	for _, p := range []string{DBPath("acme"), RunsDir("acme")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	var mode string
	if err := s.DB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q, %v", mode, err)
	}
	if v, err := s.SchemaVersion(); err != nil || v != len(migrations) {
		t.Fatalf("schema version %d, %v", v, err)
	}
	rows, err := s.DB.Query("SELECT name, kind FROM lanes ORDER BY position")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var n, k string
		if err := rows.Scan(&n, &k); err != nil {
			t.Fatal(err)
		}
		got = append(got, n+"="+k)
	}
	rows.Close()
	want := []string{"backlog=backlog", "todo=todo", "in progress=active", "review=review", "done=done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lanes %v, want %v", got, want)
	}
}

func TestOpenTwiceIsIdempotent(t *testing.T) {
	isolate(t)
	s, err := Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2 := mustOpen(t, "acme")
	var n int
	if err := s2.DB.QueryRow("SELECT COUNT(*) FROM lanes").Scan(&n); err != nil || n != 5 {
		t.Fatalf("lanes after reopen: %d, %v", n, err)
	}
}

func TestNewerSchemaIsRefused(t *testing.T) {
	isolate(t)
	s, err := Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE schema_version SET version = 99"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open("acme", []string{"all"}); err == nil || !strings.Contains(err.Error(), "newer tq") {
		t.Fatalf("want a newer-tq error, got %v", err)
	}
}

func TestWorkspaceNameIsValidated(t *testing.T) {
	isolate(t)
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "../x", "c:", " acme"} {
		if _, err := Open(bad, []string{"all"}); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestOpenExistingNeverCreates(t *testing.T) {
	isolate(t)
	if _, err := OpenExisting("ghost", []string{"all"}); !errors.Is(err, ErrNoHub) {
		t.Fatalf("want ErrNoHub, got %v", err)
	}
	if _, err := os.Stat(Dir("ghost")); !os.IsNotExist(err) {
		t.Fatalf("OpenExisting created %s", Dir("ghost"))
	}
	if _, err := OpenReadOnly("ghost"); !errors.Is(err, ErrNoHub) {
		t.Fatalf("OpenReadOnly: want ErrNoHub, got %v", err)
	}
}

func TestOpenReadOnlyDoesNotWrite(t *testing.T) {
	isolate(t)
	s, err := Open("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	ro, err := OpenReadOnly("acme")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if _, err := ro.DB.Exec("INSERT INTO settings(key, value) VALUES ('x', 'y')"); err == nil {
		t.Fatal("a read-only store accepted a write")
	}
}

func TestMaskUsesWorkspaceClasses(t *testing.T) {
	isolate(t)
	s, err := Open("acme", []string{"pii", "secrets"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	in := "mail ana@example.com token ghp_" + strings.Repeat("a", 36) // tq:allow-secret
	got := s.Mask(in)
	if strings.Contains(got, "ana@example.com") || strings.Contains(got, "ghp_aaaa") {
		t.Fatalf("not masked: %q", got)
	}
	if !strings.Contains(got, "[REDACTED:") {
		t.Fatalf("no redaction marker: %q", got)
	}
}

func TestIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID("c")
		if !ValidID("c", id) || len(id) != 14 {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
	for _, bad := range []string{"c-ABCDEFGHIJKL", "c-abc", "r-abcdefghijkl", "c-abcdefghijk1"} {
		if ValidID("c", bad) {
			t.Fatalf("%q accepted as a card id", bad)
		}
	}
}

func TestSubscribeReceivesPublishedChanges(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	ch, cancel := s.Subscribe()
	defer cancel()
	s.Publish(Change{Kind: "card", CardID: "c-abcdefghijkl"})
	c := <-ch
	if c.Workspace != "acme" || c.Kind != "card" || c.CardID != "c-abcdefghijkl" {
		t.Fatalf("change %+v", c)
	}
}

func TestChangeStampMovesOnWrite(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	before, err := s.ChangeStamp()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO activity(ts, workspace, actor, verb, data) VALUES ('2026-10-21T10:00:00Z','acme','x','card.create','{}')`); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ChangeStamp()
	if before == after {
		t.Fatalf("stamp did not move: %q", after)
	}
}
