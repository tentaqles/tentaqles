package gate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

type fakeGate struct {
	client *Client
	dir    string // FAKE_GATE_DIR
	root   string // the project folder the hub passes as --root
}

func newFakeGate(t *testing.T) fakeGate {
	t.Helper()
	py := testutil.Python(t)
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TQ_BUILD_ACTOR", "hub:leaked") // must never reach a state call
	t.Setenv("TQ_BUILD_TOKEN", "leaked")
	dir := t.TempDir()
	t.Setenv("FAKE_GATE_DIR", dir)
	script, _ := filepath.Abs(filepath.Join("testdata", "fake_build_gate.py"))
	raw, _ := os.ReadFile(filepath.Join("testdata", "state_pending.json"))
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return fakeGate{client: &Client{Command: append(py, script), Env: os.Environ()}, dir: dir, root: t.TempDir()}
}

func (f fakeGate) calls(t *testing.T) []map[string]any {
	fh, err := os.Open(filepath.Join(f.dir, "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	var out []map[string]any
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		var m map[string]any
		json.Unmarshal(sc.Bytes(), &m)
		out = append(out, m)
	}
	return out
}

func writeKey(t *testing.T) {
	t.Helper()
	p := KeyPath()
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStateParsesTheGateDocument(t *testing.T) {
	f := newFakeGate(t)
	st, err := f.client.State(context.Background(), f.root, "demo", false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Slug != "demo" || st.Workflow != "feature" || st.Task == nil || st.Task.Hub != "c-abcdefghijkl" || st.Task.External != nil {
		t.Fatalf("state %+v", st)
	}
	if len(st.Checkpoints) != 2 || st.Checkpoints[0].ID != "1" || st.Checkpoints[1].ID != "2b" {
		t.Fatalf("checkpoint ids %+v", st.Checkpoints)
	}
	if len(st.PendingDecisions) != 1 || st.PendingDecisions[0].Kind != "result" || st.Records != nil || len(st.Raw) == 0 {
		t.Fatalf("pending %+v records %v raw %d", st.PendingDecisions, st.Records, len(st.Raw))
	}
	withRecords, err := f.client.State(context.Background(), f.root, "demo", true)
	if err != nil || withRecords.Records == nil || withRecords.Records.Evidence[0].Results[0].Cmd != "python -m pytest tests/test_rows.py -q" {
		t.Fatalf("records %+v %v", withRecords.Records, err)
	}
	if withRecords.Records.DoD.Checks[1].OK != nil {
		t.Fatal("a skipped DoD check keeps ok=null")
	}
	for _, c := range f.calls(t) {
		if c["has_token"] == true || c["actor"] != nil {
			t.Fatalf("a state call carried hub credentials: %v", c)
		}
		if c["root"] != f.root {
			t.Fatalf("--root %v", c["root"])
		}
	}
}

func TestHaltedAcceptsStringOrObject(t *testing.T) {
	var a, b State
	json.Unmarshal([]byte(`{"halted": "review failed 3x"}`), &a)
	json.Unmarshal([]byte(`{"halted": {"reason": "behavior failed 3x after split", "checkpoint": 2}}`), &b)
	if a.Halted == nil || a.Halted.Reason != "review failed 3x" || b.Halted.Reason != "behavior failed 3x after split" || b.Halted.Checkpoint != "2" {
		t.Fatalf("%+v %+v", a.Halted, b.Halted)
	}
}

func TestNoPlanIsRecognised(t *testing.T) {
	f := newFakeGate(t)
	_, err := f.client.State(context.Background(), f.root, "unknown-slug", false)
	if !IsNoPlan(err) {
		t.Fatalf("want a no-plan error, got %v", err)
	}
}

func TestAnswerSendsAValidToken(t *testing.T) {
	f := newFakeGate(t)
	writeKey(t)
	msg, err := f.client.Answer(context.Background(), f.root, "demo", "d1a2b3c4", "approve", "looks right", "hub:renato")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "decision d1a2b3c4: approve by hub:renato") {
		t.Fatalf("message %q", msg)
	}
	st, _ := f.client.State(context.Background(), f.root, "demo", false)
	if len(st.PendingDecisions) != 0 || !st.Finished {
		t.Fatalf("after answer %+v", st)
	}
	calls := f.calls(t)
	last := calls[len(calls)-2] // the answer, before the state read above
	if last["actor"] != "hub:renato" || last["has_token"] != true {
		t.Fatalf("answer call %v", last)
	}
}

func TestAnswerAlreadyAnsweredIsRecognised(t *testing.T) {
	f := newFakeGate(t)
	writeKey(t)
	_, err := f.client.Answer(context.Background(), f.root, "demo", "d9999999", "approve", "", "hub:renato")
	if !IsNotPending(err) {
		t.Fatalf("want not-pending, got %v", err)
	}
}

func TestAnswerWithAWrongKeyIsAHumanRefusal(t *testing.T) {
	f := newFakeGate(t)
	writeKey(t)
	c := *f.client
	c.KeyPath = filepath.Join(t.TempDir(), "other.key")
	os.WriteFile(c.KeyPath, []byte("not the gate key"), 0o600)
	if _, err := c.Answer(context.Background(), f.root, "demo", "d1a2b3c4", "approve", "", "hub:renato"); !IsHumanRefusal(err) {
		t.Fatalf("want a human-only refusal, got %v", err)
	}
}

func TestAnswerWithoutAKeySaysAnswerInTheTerminal(t *testing.T) {
	f := newFakeGate(t)
	_, err := f.client.Answer(context.Background(), f.root, "demo", "d1a2b3c4", "approve", "", "hub:renato")
	if err == nil || !strings.Contains(err.Error(), "build-gate answer") {
		t.Fatalf("err %v", err)
	}
}

func TestSlugsNewerThan(t *testing.T) {
	root := t.TempDir()
	for _, s := range []string{"old", "new"} {
		os.MkdirAll(filepath.Join(root, ".claude", "build", s), 0o700)
	}
	past := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(root, ".claude", "build", "old"), past, past)
	got := Slugs(root, time.Now().Add(-10*time.Minute))
	if len(got) != 1 || got[0] != "new" {
		t.Fatalf("slugs %v", got)
	}
}

func TestUnavailableReturnsItsError(t *testing.T) {
	u := Unavailable{Err: os.ErrNotExist}
	if _, err := u.State(context.Background(), ".", "x", false); err != os.ErrNotExist {
		t.Fatal(err)
	}
	if u.Slugs(".", time.Time{}) != nil {
		t.Fatal("no slugs")
	}
}

func TestAnswerRecordedThenFailedReturnsMessageAndError(t *testing.T) {
	f := newFakeGate(t)
	writeKey(t)
	msg, err := f.client.Answer(context.Background(), f.root, "demo", "d1a2b3c4", "approve", "!record-fail", "hub:renato")
	var ge *GateError
	if !errors.As(err, &ge) || ge.Code != 1 {
		t.Fatalf("want GateError code 1, got %v", err)
	}
	if !strings.Contains(msg, "decision d1a2b3c4: approve by hub:renato") || !strings.Contains(msg, "finish did NOT apply") {
		t.Fatalf("message %q", msg)
	}
	st, _ := f.client.State(context.Background(), f.root, "demo", false)
	if len(st.PendingDecisions) != 0 {
		t.Fatal("the fake recorded the answer before failing")
	}
}

func TestExitTwoClassification(t *testing.T) {
	f := newFakeGate(t)
	writeKey(t)
	ctx := context.Background()
	_, err := f.client.Answer(ctx, f.root, "demo", "d1a2b3c4", "approve", "!needs-note", "hub:renato")
	if !IsNeedsNote(err) || IsHumanRefusal(err) {
		t.Fatalf("needs-note misclassified: %v", err)
	}
	_, err = f.client.Answer(ctx, f.root, "demo", "d1a2b3c4", "approve", "!actor-mismatch", "hub:renato")
	if !IsHumanRefusal(err) || IsNeedsNote(err) {
		t.Fatalf("actor mismatch misclassified: %v", err)
	}
	human := &GateError{Code: 2, Msg: "build-gate: this step needs a human at an interactive terminal (PowerShell). Refusing."}
	if !IsHumanRefusal(human) || IsNeedsNote(human) {
		t.Fatal("human-only refusal misclassified")
	}
	if IsHumanRefusal(&GateError{Code: 2, Msg: "something else"}) {
		t.Fatal("bare exit 2 is not a human refusal")
	}
}

func TestNilEnvInheritsProcessEnvironment(t *testing.T) {
	f := newFakeGate(t)
	c := *f.client
	c.Env = nil
	if _, err := c.State(context.Background(), f.root, "demo", false); err != nil {
		t.Fatalf("nil Env must inherit the process environment: %v", err)
	}
}
