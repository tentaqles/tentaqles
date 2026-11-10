package cli

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// openAcme creates acme's store the way a lobe verb does.
func openAcme(t *testing.T) {
	t.Helper()
	if _, err := store.Open("acme", store.ReadWrite); err != nil {
		t.Fatal(err)
	}
}

// writeDay writes the day file of acme's event log with the given lines.
func writeDay(t *testing.T, day string, lines ...string) {
	t.Helper()
	p := filepath.Join(paths.LobeDir("acme"), "events", "events-"+day+".jsonl")
	mustWrite(t, p, strings.Join(lines, "\n")+"\n")
}

// eventLine is one stored line whose n field says where it sits.
func eventLine(n int) string {
	return fmt.Sprintf(`{"v":1,"id":"evt_%04d","kind":"fail","n":%d}`, n, n)
}

// numbers returns the n fields of the printed lines, in the order printed.
func numbers(t *testing.T, out string) []int {
	t.Helper()
	var ns []int
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if l == "" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(l[strings.LastIndex(l, `"n":`)+4:], "%d", &n); err != nil {
			t.Fatalf("line %q holds no n: %v", l, err)
		}
		ns = append(ns, n)
	}
	return ns
}

func TestEventsOrderLimitEmpty(t *testing.T) {
	f := newHome(t)
	cwd := f.client("acme")

	// An empty log prints nothing and is no error.
	var buf bytes.Buffer
	if err := Events(deps(false, nil, cwd), "", &buf); err != nil {
		t.Fatalf("empty log: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("empty log printed %q", buf.String())
	}

	// Older day: 30 lines; newer day: 5 lines. The newest 20 are 16..30 of the older day, then 31..35.
	var older, newer []string
	for n := 1; n <= 30; n++ {
		older = append(older, eventLine(n))
	}
	for n := 31; n <= 35; n++ {
		newer = append(newer, eventLine(n))
	}
	writeDay(t, "20261107", older...)
	writeDay(t, "20261108", newer...)

	buf.Reset()
	if err := Events(deps(false, nil, cwd), "", &buf); err != nil {
		t.Fatal(err)
	}
	got := numbers(t, buf.String())
	if len(got) != EventsLimit {
		t.Fatalf("printed %d lines, want %d", len(got), EventsLimit)
	}
	for i, n := range got {
		if want := 16 + i; n != want {
			t.Fatalf("line %d holds n=%d, want %d (oldest of the selection first)", i, n, want)
		}
	}
	// The lines are printed as they are on disk.
	if !strings.HasPrefix(buf.String(), eventLine(16)+"\n") {
		t.Fatalf("first line is not the stored bytes: %q", buf.String()[:60])
	}

	// Fewer lines than the limit: all of them, in order.
	f2 := newHome(t)
	openAcme(t)
	writeDay(t, "20261109", eventLine(1), eventLine(2), eventLine(3))
	buf.Reset()
	if err := Events(deps(false, nil, f2.client("acme")), "", &buf); err != nil {
		t.Fatal(err)
	}
	if got := numbers(t, buf.String()); fmt.Sprint(got) != "[1 2 3]" {
		t.Fatalf("printed %v, want [1 2 3]", got)
	}
}

func TestEventsRefusesOtherLoginInsideAgent(t *testing.T) {
	f := newHome(t)
	openAcme(t)
	env := map[string]string{"CLAUDE_CONFIG_DIR": paths.IdentityDir("globex", "claude")}
	var buf bytes.Buffer
	if err := Events(deps(true, env, f.client("acme")), "", &buf); err == nil {
		t.Fatal("events under another login was not refused")
	}
	if buf.Len() != 0 {
		t.Fatalf("refused events printed %q", buf.String())
	}
}
