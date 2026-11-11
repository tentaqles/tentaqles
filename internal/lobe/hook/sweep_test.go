package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// sessionEndJSON is the JSON a host sends when a session ends in cwd (Claude Code's SessionEnd; Codex's
// has the same keys that the hook reads). cwd is encoded as JSON, so a Windows path is valid.
func sessionEndJSON(cwd string) string {
	b, err := json.Marshal(map[string]string{
		"session_id":      "s1",
		"hook_event_name": "SessionEnd",
		"cwd":             cwd,
		"reason":          "other",
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// expiredDay is a day file name 20200101: far older than any retention_days.
const expiredDay = "events-20200101.jsonl"

// todayDay is the name of today's day file, which a sweep never deletes.
func todayDay() string { return "events-" + time.Now().UTC().Format("20060102") + ".jsonl" }

// eventsDir is client's events folder in its store.
func eventsDir(client string) string { return filepath.Join(paths.LobeDir(client), "events") }

// makeStore creates client's store, the lobe folder and its store.json, as any earlier write of the lobe
// does. A session end sweeps only an existing store, so a fixture with day files needs one.
func makeStore(t *testing.T, client string) {
	t.Helper()
	if _, err := store.Open(client, store.ReadWrite); err != nil {
		t.Fatal(err)
	}
}

// TestLobeSessionEndSweeps: a session end deletes the expired day file of the client, prints nothing, and
// exits 0 within the session-end cutoff. A stdin that never closes ends at the cutoff and deletes nothing.
// Capture off does not stop the sweep (F-06 step 1).
func TestLobeSessionEndSweeps(t *testing.T) {
	t.Run("deletes the expired file", func(t *testing.T) {
		f := newHome(t)
		setLogin(t, f, "acme")
		makeStore(t, "acme")
		mustWrite(t, filepath.Join(eventsDir("acme"), expiredDay), "")
		mustWrite(t, filepath.Join(eventsDir("acme"), todayDay()), "")

		var buf bytes.Buffer
		start := time.Now()
		handled, code := Main([]string{"lobe-hook", "claude", "session-end"},
			strings.NewReader(sessionEndJSON(f.app("acme"))), &buf)
		elapsed := time.Since(start)
		if !handled || code != 0 {
			t.Fatalf("Main = handled %v, code %d; want true, 0", handled, code)
		}
		if buf.Len() != 0 {
			t.Errorf("stdout %q, want 0 bytes", buf.String())
		}
		if elapsed > 1250*time.Millisecond {
			t.Errorf("session end took %v, want within 1.25s", elapsed)
		}
		if exists(filepath.Join(eventsDir("acme"), expiredDay)) {
			t.Errorf("the expired day file is still there")
		}
		if !exists(filepath.Join(eventsDir("acme"), todayDay())) {
			t.Errorf("today's day file was deleted")
		}
	})

	t.Run("a stdin that never closes deletes nothing", func(t *testing.T) {
		f := newHome(t)
		setLogin(t, f, "acme")
		makeStore(t, "acme")
		mustWrite(t, filepath.Join(eventsDir("acme"), expiredDay), "")

		pr, pw := io.Pipe()
		defer pw.Close()
		defer pr.Close()
		var buf bytes.Buffer
		start := time.Now()
		handled, code := Main([]string{"lobe-hook", "claude", "session-end"}, pr, &buf)
		elapsed := time.Since(start)
		if !handled || code != 0 {
			t.Fatalf("Main = handled %v, code %d; want true, 0", handled, code)
		}
		if buf.Len() != 0 {
			t.Errorf("stdout %q, want 0 bytes", buf.String())
		}
		if elapsed > 1250*time.Millisecond {
			t.Errorf("stalled session end took %v, want within 1.25s", elapsed)
		}
		if !exists(filepath.Join(eventsDir("acme"), expiredDay)) {
			t.Errorf("the expired day file was deleted by a run that never read its payload")
		}
	})

	t.Run("capture off still sweeps", func(t *testing.T) {
		f := newHome(t)
		setLogin(t, f, "acme")
		appendManifestLobe(t, f, "acme", "capture: off")
		makeStore(t, "acme")
		mustWrite(t, filepath.Join(eventsDir("acme"), expiredDay), "")

		var buf bytes.Buffer
		if _, code := Main([]string{"lobe-hook", "claude", "session-end"},
			strings.NewReader(sessionEndJSON(f.app("acme"))), &buf); code != 0 || buf.Len() != 0 {
			t.Fatalf("code %d, stdout %q; want 0 and nothing", code, buf.String())
		}
		if exists(filepath.Join(eventsDir("acme"), expiredDay)) {
			t.Errorf("with capture off the expired day file is still there")
		}
	})

	t.Run("TQ_LOBE_NO_HOOKS=1 deletes nothing", func(t *testing.T) {
		f := newHome(t)
		setLogin(t, f, "acme")
		t.Setenv("TQ_LOBE_NO_HOOKS", "1")
		makeStore(t, "acme")
		mustWrite(t, filepath.Join(eventsDir("acme"), expiredDay), "")

		if out := runHook(t, "session-end", sessionEndJSON(f.app("acme"))); out != "" {
			t.Errorf("stdout %q, want nothing", out)
		}
		if !exists(filepath.Join(eventsDir("acme"), expiredDay)) {
			t.Errorf("the switch is on and the expired day file was deleted")
		}
	})
}

// TestLobeSessionEndNoStoreNoFolder: a client with no lobe folder gets none at session end, with capture off
// and with capture on: the sweep has nothing to delete, so it opens the store read-only and creates nothing.
func TestLobeSessionEndNoStoreNoFolder(t *testing.T) {
	for _, capture := range []string{"capture: off", "capture: on"} {
		t.Run(capture, func(t *testing.T) {
			f := newHome(t)
			setLogin(t, f, "acme")
			appendManifestLobe(t, f, "acme", capture)

			var buf bytes.Buffer
			if _, code := Main([]string{"lobe-hook", "claude", "session-end"},
				strings.NewReader(sessionEndJSON(f.app("acme"))), &buf); code != 0 || buf.Len() != 0 {
				t.Fatalf("code %d, stdout %q; want 0 and nothing", code, buf.String())
			}
			if exists(paths.LobeDir("acme")) {
				t.Errorf("session end created the lobe folder of a client with no store")
			}
		})
	}
}
