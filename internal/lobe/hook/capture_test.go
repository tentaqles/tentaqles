package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// appendManifestLobe sets the lobe key of client's manifest to body (one line, indented under lobe:).
// Any lobe block the manifest already has is cut first, so a test can switch a value back and forth.
func appendManifestLobe(t *testing.T, f fixture, client, body string) {
	t.Helper()
	p := filepath.Join(f.identity(client), "manifest.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if i := strings.Index(text, "lobe:"); i >= 0 {
		text = text[:i]
	}
	mustWrite(t, p, text+"lobe:\n  "+body+"\n")
}

// TestLobeCaptureOffRecordsNothing: with capture off, a stop run over a transcript that holds a
// rejected call reads nothing, writes no event, no cursor and no store folder.
func TestLobeCaptureOffRecordsNothing(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	appendManifestLobe(t, f, "acme", "capture: off")
	app := f.app("acme")
	T := acmeTranscript()
	appendText(t, T, rejectedEdit(t, "toolu_off1", "ZZZ"))
	if out := runHook(t, "stop", stopJSON(t, app, T)); out != "" {
		t.Fatalf("stop wrote %q to stdout, want nothing", out)
	}
	if ls := lines(t, "acme"); len(ls) != 0 {
		t.Fatalf("%d event lines with capture off, want 0", len(ls))
	}
	if exists(paths.LobeDir("acme")) {
		t.Fatalf("a lobe folder was created with capture off")
	}
}

// TestLobeCaptureSwitchedOnRecordsOnlyNewFailures: a failure during the off period is never recorded; once
// capture is back on, the next failure is recorded and nothing from the off period is. The off-period
// failure is a failed-call payload; TestLobeCaptureOffRejectedNeverRecovered covers the transcript.
func TestLobeCaptureSwitchedOnRecordsOnlyNewFailures(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")

	appendManifestLobe(t, f, "acme", "capture: off")
	if out := runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_off", "x --off", "Exit code 1\nfail off")); out != "" {
		t.Fatalf("off: stdout %q, want nothing", out)
	}
	if n := len(lines(t, "acme")); n != 0 {
		t.Fatalf("off: %d lines, want 0", n)
	}

	appendManifestLobe(t, f, "acme", "capture: on")
	if out := runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_on", "x --on", "Exit code 1\nfail on")); out != "" {
		t.Fatalf("on: stdout %q, want nothing", out)
	}
	ls := lines(t, "acme")
	if len(ls) != 1 {
		t.Fatalf("on: %d lines, want 1", len(ls))
	}
	if got := decodeLogged(t, ls)[0]; got.ID != eventlog.ID("claude", "s1", "toolu_on") {
		t.Errorf("on: id = %s, want the toolu_on call", got.ID)
	}
	for _, l := range ls {
		if strings.Contains(l, "--off") {
			t.Errorf("a line from the off period was recorded: %s", l)
		}
	}
}

// TestLobeCaptureMaybeRecordsNothing: a wrong capture value reads as off, so no event is recorded for a
// failed call or a rejected call in the transcript.
func TestLobeCaptureMaybeRecordsNothing(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	appendManifestLobe(t, f, "acme", "capture: maybe")
	app := f.app("acme")
	if out := runHook(t, evPost, failureJSON(f)); out != "" {
		t.Fatalf("post: stdout %q, want nothing", out)
	}
	T := acmeTranscript()
	appendText(t, T, rejectedEdit(t, "toolu_maybe", "ZZZ"))
	if out := runHook(t, "stop", stopJSON(t, app, T)); out != "" {
		t.Fatalf("stop: stdout %q, want nothing", out)
	}
	if ls := lines(t, "acme"); len(ls) != 0 {
		t.Fatalf("%d event lines with capture maybe, want 0", len(ls))
	}
	if exists(paths.LobeDir("acme")) {
		t.Fatalf("a lobe folder was created with capture maybe")
	}
}

// TestLobeCaptureOffRejectedNeverRecovered: a call rejected while capture is off is never recorded, not
// even by the first stop after capture is on again. With the store present, a capture-off stop moves the
// session's cursor to the end of the transcript; a call rejected after the switch is recorded.
func TestLobeCaptureOffRejectedNeverRecovered(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	T := acmeTranscript()

	// The store exists: one failure recorded while capture is on.
	runHook(t, evPost, payloadJSON("s0", app, "Bash", "toolu_first", "x", "Exit code 1\nfail"))
	if n := len(lines(t, "acme")); n != 1 {
		t.Fatalf("setup: %d lines, want 1", n)
	}

	appendManifestLobe(t, f, "acme", "capture: off")
	appendText(t, T, rejectedEdit(t, "toolu_during_off", "ZZZ"))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := len(lines(t, "acme")); n != 1 {
		t.Fatalf("off: %d lines, want 1", n)
	}

	appendManifestLobe(t, f, "acme", "capture: on")
	runHook(t, "stop", stopJSON(t, app, T))
	if n := idCount(t, "acme", eventlog.ID("claude", "s1", "toolu_during_off")); n != 0 {
		t.Fatalf("the call rejected while capture was off was recorded %d times after the switch", n)
	}

	appendText(t, T, rejectedEdit(t, "toolu_after_on", "QQQ"))
	runHook(t, "stop", stopJSON(t, app, T))
	if n := idCount(t, "acme", eventlog.ID("claude", "s1", "toolu_after_on")); n != 1 {
		t.Fatalf("the call rejected after the switch is logged %d times, want 1", n)
	}
	if n := idCount(t, "acme", eventlog.ID("claude", "s1", "toolu_during_off")); n != 0 {
		t.Fatalf("the off-period call appeared later: %d", n)
	}
}
