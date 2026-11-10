package eventlog

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// dedupeBase is a full failure line at 10:00:00 with the input {"command":"make"}.
func dedupeBase() Event {
	return Event{
		V: 1, ID: "evt_a", At: "2026-11-10T10:00:00Z", Session: "s1", Host: "claude", Tool: "Bash",
		Kind: KindFail, Cwd: ".", FP: "fp_1",
		Input: scrub.Input(json.RawMessage(`{"command":"make"}`)),
		Error: scrub.Text("make: *** no rule"),
	}
}

// tailOf is the line of e as it is stored: its input is the stored JSON string.
func tailOf(e Event) TailEvent {
	return TailEvent{
		V: e.V, ID: e.ID, At: e.At, Session: e.Session, Host: e.Host, Tool: e.Tool, Kind: e.Kind,
		Input: e.Input.String(), FP: e.FP, Dup: e.Dup,
	}
}

// later is e with a new call id, ten seconds after its time.
func later(e Event, id string) Event {
	e.ID = id
	e.At = "2026-11-10T10:00:10Z"
	return e
}

func TestLobeDedupeCheck(t *testing.T) {
	base := dedupeBase()
	// A first line with no input and no error, for the reference-line case.
	bare := base
	bare.Input = scrub.Clean{}
	bare.Error = scrub.Clean{}

	diffFP := later(base, "evt_b")
	diffFP.FP = "fp_2"
	diffInput := later(base, "evt_b")
	diffInput.Input = scrub.Input(json.RawMessage(`{"command":"make test"}`))
	diffKind := later(base, "evt_b")
	diffKind.Kind = KindOK
	noSession := later(base, "evt_b")
	noSession.Session = ""
	noSessionFirst := bare
	noSessionFirst.Session = ""

	cases := []struct {
		name      string
		tail      []TailEvent
		e         Event
		wantV     Verdict
		wantFirst string
	}{
		{"empty tail", nil, later(base, "evt_b"), VerdictNew, ""},
		{"replay: same session and id", []TailEvent{tailOf(base)}, base, VerdictReplay, ""},
		{"repeat: same content, new id", []TailEvent{tailOf(base)}, later(base, "evt_b"), VerdictRepeat, "evt_a"},
		{"different fp", []TailEvent{tailOf(base)}, diffFP, VerdictNew, ""},
		{"different input", []TailEvent{tailOf(base)}, diffInput, VerdictNew, ""},
		{"different kind", []TailEvent{tailOf(base)}, diffKind, VerdictNew, ""},
		{"tail holds only a reference line for that content", []TailEvent{tailOf(Reference(bare, "evt_a"))}, later(bare, "evt_b"), VerdictNew, ""},
		{"empty session: no check", []TailEvent{tailOf(noSessionFirst)}, noSession, VerdictNew, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, first := Check(c.tail, c.e)
			if v != c.wantV || first != c.wantFirst {
				t.Errorf("Check = (%q, %q), want (%q, %q)", v, first, c.wantV, c.wantFirst)
			}
		})
	}
}

func TestLobeDedupeWindow(t *testing.T) {
	base := dedupeBase()
	at := func(s string) Event {
		e := later(base, "evt_b")
		e.At = s
		return e
	}
	if v, _ := Check([]TailEvent{tailOf(base)}, at("2026-11-10T10:05:00Z")); v != VerdictRepeat {
		t.Errorf("300 seconds apart: verdict %q, want repeat", v)
	}
	if v, _ := Check([]TailEvent{tailOf(base)}, at("2026-11-10T10:05:01Z")); v != VerdictNew {
		t.Errorf("301 seconds apart: verdict %q, want new", v)
	}
	// A full line later than the event is not "before" it.
	if v, _ := Check([]TailEvent{tailOf(base)}, at("2026-11-10T09:59:50Z")); v != VerdictNew {
		t.Errorf("a line after the event: verdict %q, want new", v)
	}
	// A time that does not parse is never a repeat.
	bad := base
	bad.At = "not a time"
	if v, _ := Check([]TailEvent{tailOf(bad)}, later(base, "evt_b")); v != VerdictNew {
		t.Errorf("unparseable line time: verdict %q, want new", v)
	}
	if v, _ := Check([]TailEvent{tailOf(base)}, at("not a time")); v != VerdictNew {
		t.Errorf("unparseable event time: verdict %q, want new", v)
	}
}

func TestLobeReferenceLine(t *testing.T) {
	base := dedupeBase()
	ref := Reference(base, "evt_a")
	if ref.Input.String() != "" || ref.Error.String() != "" || ref.Cwd != "" || ref.Repo != "" || ref.Exit != nil {
		t.Errorf("Reference keeps input, error, cwd, repo or exit: %+v", ref)
	}
	if ref.Dup != "evt_a" {
		t.Errorf("Reference Dup = %q, want evt_a", ref.Dup)
	}

	line, err := ReferenceLine(Reference(later(base, "evt_b"), "evt_a"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"at", "dup", "fp", "host", "id", "kind", "session", "tool", "v"}
	if got := keysOf(t, line.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("reference keys = %v, want %v\n%s", got, want, line)
	}
	if n := len(line.String()); n >= 250 {
		t.Errorf("reference line is %d bytes, want under 250: %s", n, line)
	}

	noFP := base
	noFP.FP = ""
	line, err = ReferenceLine(Reference(noFP, "evt_a"))
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"at", "dup", "host", "id", "kind", "session", "tool", "v"}
	if got := keysOf(t, line.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("reference keys without fp = %v, want %v", got, want)
	}
}

// keysOf returns the sorted top-level keys of one JSON object.
func keysOf(t *testing.T, s string) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not a JSON object: %v: %s", err, s)
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestLobeDedupeLongInputAsStored: an input cut at 4000 characters on the way in is compared as the
// stored text. Decoding the tail line does not scrub it again, so the repeat is found.
func TestLobeDedupeLongInputAsStored(t *testing.T) {
	long := scrub.Text(strings.Repeat("a", 5000))
	first := dedupeBase()
	first.Input = long
	stored := tailOf(first)
	if stored.Input != long.String() {
		t.Fatalf("stored input %d bytes, want the scrubbed %d", len(stored.Input), len(long.String()))
	}
	v, id := Check([]TailEvent{stored}, later(first, "evt_b"))
	if v != VerdictRepeat || id != "evt_a" {
		t.Errorf("Check = (%q, %q), want (repeat, evt_a)", v, id)
	}
}
