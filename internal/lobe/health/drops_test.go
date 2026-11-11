package health

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// writeDropLines writes one drops line per time in ats, with reason why, to the drops file of day (UTC).
func writeDropLines(t *testing.T, st *store.Store, day time.Time, why string, ats ...time.Time) {
	t.Helper()
	p := filepath.Join(st.Dir(), filepath.FromSlash(eventlog.DropsFileFor(day)))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, at := range ats {
		fmt.Fprintf(&b, `{"at":"%s","why":"%s"}`+"\n", at.UTC().Format(time.RFC3339), why)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// droppedStore opens a fresh store for client name (its identity folder is made first, as a render does)
// and returns it with the time the test works at. Each test uses its own name, so the stores do not share
// drops files.
func droppedStore(t *testing.T, name string) (*store.Store, time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(paths.IdentitiesRoot(), name), 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(name, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	return st, time.Now().UTC()
}

// TestDroppedFinding_Threshold: 4 recent drops give no finding, 5 give one warning that names the client
// and counts per reason.
func TestDroppedFinding_Threshold(t *testing.T) {
	t.Run("four lines give none", func(t *testing.T) {
		st, now := droppedStore(t, "dropfour")
		writeDropLines(t, st, now, "full", now.Add(-time.Minute), now.Add(-2*time.Minute), now.Add(-3*time.Minute), now.Add(-4*time.Minute))
		if fs := storeFindings(Client{Name: st.Client()}, now); len(fs) != 0 {
			t.Fatalf("4 drops gave findings: %+v", fs)
		}
	})
	t.Run("five lines give one finding with the counts", func(t *testing.T) {
		st, now := droppedStore(t, "dropfive")
		writeDropLines(t, st, now, "full", now.Add(-time.Minute), now.Add(-2*time.Minute), now.Add(-3*time.Minute))
		writeDropLines(t, st, now, "disk", now.Add(-4*time.Minute), now.Add(-5*time.Minute))
		fs := storeFindings(Client{Name: st.Client()}, now)
		if len(fs) != 1 {
			t.Fatalf("5 drops gave %d findings, want 1: %+v", len(fs), fs)
		}
		f := fs[0]
		if f.Code != CodeEventsDropped || f.Level != "warn" || f.Client != "dropfive" {
			t.Fatalf("finding = %+v, want a warn %s for dropfive", f, CodeEventsDropped)
		}
		if !strings.Contains(f.Msg, "(disk 2, full 3)") {
			t.Errorf("message does not count per reason: %q", f.Msg)
		}
		if f.Fix == "" {
			t.Error("finding has no fix line")
		}
	})
	t.Run("yesterday's file counts too", func(t *testing.T) {
		st, now := droppedStore(t, "dropyesterday")
		yesterday := now.AddDate(0, 0, -1)
		writeDropLines(t, st, yesterday, "big", now.Add(-time.Hour), now.Add(-2*time.Hour), now.Add(-3*time.Hour), now.Add(-4*time.Hour), now.Add(-5*time.Hour))
		if fs := storeFindings(Client{Name: st.Client()}, now); len(fs) != 1 || fs[0].Code != CodeEventsDropped {
			t.Fatalf("5 drops in yesterday's file gave %+v", fs)
		}
	})
}

// TestDroppedFinding_OldLinesNotCounted: a line older than 24 hours is not counted, so four recent lines
// with three old ones give no finding.
func TestDroppedFinding_OldLinesNotCounted(t *testing.T) {
	st, now := droppedStore(t, "dropold")
	old := now.Add(-25 * time.Hour)
	writeDropLines(t, st, now, "full", old, old.Add(-time.Minute), old.Add(-2*time.Minute))
	writeDropLines(t, st, now, "full", now.Add(-time.Minute), now.Add(-2*time.Minute), now.Add(-3*time.Minute), now.Add(-4*time.Minute))
	if fs := storeFindings(Client{Name: st.Client()}, now); len(fs) != 0 {
		t.Fatalf("3 lines older than 24 hours were counted: %+v", fs)
	}
}
