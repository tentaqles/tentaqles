package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// dedupeHelperEnv names the variable that makes TestDedupeHelperProcess send one failure.
const dedupeHelperEnv = "LOBE_DEDUPE_HELPER"

// fixClock makes the hook's clock read at for one test, restored at its end.
func fixClock(t *testing.T, at time.Time) {
	t.Helper()
	old := nowFn
	nowFn = func() time.Time { return at }
	t.Cleanup(func() { nowFn = old })
}

// TestDedupeHelperProcess is not a test: TestLobeDedupeRace re-runs the test binary on it with
// LOBE_DEDUPE_HELPER=send. It then runs one failure through Main, reading the payload from stdin.
// Without the variable it returns at once.
func TestDedupeHelperProcess(t *testing.T) {
	if os.Getenv(dedupeHelperEnv) != "send" {
		return
	}
	// Many processes starting at once can be slow; a run that the cutoff ends would lose its line.
	captureCutoff, injectCutoff, watchdogSlack = 10*time.Second, 10*time.Second, time.Second
	Main([]string{"lobe-hook", "claude", evPost}, os.Stdin, os.Stdout)
}

// TestLobeDedupeReplay: the same payload twice gives one line.
func TestLobeDedupeReplay(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	payload := failureJSON(f)
	for i := 0; i < 2; i++ {
		if out := runHook(t, evPost, payload); out != "" {
			t.Errorf("run %d: stdout %q, want nothing", i+1, out)
		}
	}
	if ls := lines(t, "acme"); len(ls) != 1 {
		t.Fatalf("the same payload twice gave %d lines, want 1: %q", len(ls), ls)
	}
}

// TestLobeDedupeRepeat: the same failure under two call ids, 10 seconds apart, gives two lines. The
// second is a reference line: dup is the first id, no input and no error, under 250 bytes.
func TestLobeDedupeRepeat(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	start := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	const errText = "Exit code 1\nnpm error Missing script: buld"

	fixClock(t, start)
	runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_01", "npm run buld", errText))
	fixClock(t, start.Add(10*time.Second))
	runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_02", "npm run buld", errText))

	ls := lines(t, "acme")
	if len(ls) != 2 {
		t.Fatalf("%d lines, want 2: %q", len(ls), ls)
	}
	first, second := ls[0], ls[1]
	var fl, sl map[string]any
	if err := json.Unmarshal([]byte(first), &fl); err != nil {
		t.Fatalf("first line does not parse: %v", err)
	}
	if err := json.Unmarshal([]byte(second), &sl); err != nil {
		t.Fatalf("second line does not parse: %v", err)
	}
	if want := eventlog.ID("claude", "s1", "toolu_01"); fl["id"] != want {
		t.Errorf("first id = %v, want %s", fl["id"], want)
	}
	if want := eventlog.ID("claude", "s1", "toolu_02"); sl["id"] != want {
		t.Errorf("second id = %v, want %s", sl["id"], want)
	}
	if sl["dup"] != fl["id"] {
		t.Errorf("second dup = %v, want the first id %v", sl["dup"], fl["id"])
	}
	if _, has := sl["input"]; has {
		t.Errorf("reference line has an input: %s", second)
	}
	if _, has := sl["error"]; has {
		t.Errorf("reference line has an error: %s", second)
	}
	if sl["kind"] != "fail" || sl["fp"] == nil || sl["fp"] != fl["fp"] {
		t.Errorf("reference line kind/fp = %v/%v, want fail and the first line's fp %v", sl["kind"], sl["fp"], fl["fp"])
	}
	if n := len(second); n >= 250 {
		t.Errorf("reference line is %d bytes, want under 250: %s", n, second)
	}
}

// TestLobeDedupeOtherSession: the same content in another session is never a repeat: two full lines.
func TestLobeDedupeOtherSession(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	const errText = "Exit code 1\nnpm error Missing script: buld"
	runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_01", "npm run buld", errText))
	runHook(t, evPost, payloadJSON("s2", app, "Bash", "toolu_01", "npm run buld", errText))

	ls := lines(t, "acme")
	if len(ls) != 2 {
		t.Fatalf("%d lines, want 2: %q", len(ls), ls)
	}
	got := decodeLogged(t, ls)
	if got[1].Input != `{"command":"npm run buld"}` || got[1].Error == "" {
		t.Errorf("second session's line is not a full line: %+v", got[1])
	}
	var second map[string]any
	if err := json.Unmarshal([]byte(ls[1]), &second); err != nil {
		t.Fatal(err)
	}
	if _, has := second["dup"]; has {
		t.Errorf("second session's line has a dup: %s", ls[1])
	}
}

// TestLobeDedupeRace: 20 processes send the same payload at once. Whether each one got the lock or not,
// a reader that goes through eventlog.Scan counts one event.
func TestLobeDedupeRace(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	payload := failureJSON(f)

	const senders = 20
	cmds := make([]*exec.Cmd, senders)
	outs := make([]*bytes.Buffer, senders)
	for i := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestDedupeHelperProcess$")
		cmd.Env = append(os.Environ(), dedupeHelperEnv+"=send")
		cmd.Stdin = strings.NewReader(payload)
		outs[i] = &bytes.Buffer{}
		cmd.Stdout = outs[i]
		cmd.Stderr = outs[i]
		cmds[i] = cmd
		if err := cmd.Start(); err != nil {
			t.Fatalf("start sender %d: %v", i, err)
		}
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("sender %d: %v\n%s", i, err, outs[i].String())
		}
	}

	st, err := store.Open("acme", store.ReadOnly)
	if err != nil {
		t.Fatalf("open acme: %v", err)
	}
	var got []string
	if _, _, err := eventlog.Scan(st, func(line []byte) bool {
		got = append(got, string(line))
		return true
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	t.Logf("%d senders left %d lines on disk", senders, len(lines(t, "acme")))
	if len(got) != 1 {
		t.Fatalf("a reader counts %d events for 20 senders of one payload, want 1: %q", len(got), got)
	}
	if !strings.Contains(got[0], `"id":"evt_9bde2e09"`) {
		t.Errorf("the event is not the payload's: %s", got[0])
	}
}

// TestLobeDedupeWindowHook: the same failure under two call ids, 301 seconds apart, gives two full lines
// through the hook, the clock read from the nowFn seam. (The eventlog test covers the check itself.)
func TestLobeDedupeWindowHook(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	start := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	const errText = "Exit code 1\nnpm error Missing script: buld"

	fixClock(t, start)
	runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_01", "npm run buld", errText))
	fixClock(t, start.Add(301*time.Second))
	runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_02", "npm run buld", errText))

	ls := lines(t, "acme")
	if len(ls) != 2 {
		t.Fatalf("%d lines, want 2 full lines: %q", len(ls), ls)
	}
	for i, l := range ls {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line %d does not parse: %v", i, err)
		}
		if _, has := m["dup"]; has {
			t.Errorf("line %d has a dup, want a full line: %s", i, l)
		}
		if _, has := m["input"]; !has {
			t.Errorf("line %d has no input, want a full line: %s", i, l)
		}
	}
}

// TestLobeDedupeHookThenStop: a failure the hook reported (post-tool-use-failure for toolu_x), then a stop
// whose transcript holds the same call rejected with the same id: the stop writes nothing, so one line.
func TestLobeDedupeHookThenStop(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	transcript := acmeTranscript()

	runHook(t, evPost, payloadJSON("s1", app, "Edit", "toolu_x", "", "String to replace not found in file."))
	if n := len(lines(t, "acme")); n != 1 {
		t.Fatalf("after the failure hook: %d lines, want 1", n)
	}
	appendText(t, transcript, rejectedEdit(t, "toolu_x", "ZZZ"))
	runHook(t, "stop", stopJSON(t, app, transcript))

	ls := lines(t, "acme")
	if len(ls) != 1 {
		t.Fatalf("after the stop: %d lines, want exactly 1: %q", len(ls), ls)
	}
	if want := eventlog.ID("claude", "s1", "toolu_x"); decodeLogged(t, ls)[0].ID != want {
		t.Errorf("the line's id is not the call's: %s", ls[0])
	}
}
