// Ported from agentmemory src/functions/dedup.ts at 77f45358a8300299158e3a416e9010176408ea87 (Apache-2.0).

package eventlog

import "time"

// Verdict is what Check decides for a new event.
type Verdict string

const (
	// VerdictNew means the event is written in full.
	VerdictNew Verdict = "new"
	// VerdictReplay means the same session already holds a line with this id: nothing is written.
	VerdictReplay Verdict = "replay"
	// VerdictRepeat means a full line of the same call content came shortly before: a reference line
	// is written instead.
	VerdictRepeat Verdict = "repeat"
	// VerdictDropped means the event was not written: a limit of S-07 refused it (full or big), or its
	// write failed (disk). A drops line with the reason was written, best effort. It is returned by
	// AppendChecked only; Check never returns it.
	VerdictDropped Verdict = "dropped"
)

// repeatWindow is how far back a full line may be and still make a repeat.
const repeatWindow = 300 * time.Second

// Check decides what to do with e given the tail of the day file, oldest first. It is pure: it reads
// nothing and writes nothing. The tail is compared as stored: a line's input is its stored JSON string,
// and e's input is e.Input.String().
//
// An event with no session is new. A tail line with the same session and id makes a replay, and
// firstID is then empty. A repeat is a full line (one with no dup) with the same session, tool, kind,
// input and fp whose at is 0 to 300 seconds before e.At; firstID is that line's id. Of several such
// lines the most recent one is taken. A line whose at does not parse never makes a repeat, and
// neither does an event whose own at does not parse.
func Check(tail []TailEvent, e Event) (v Verdict, firstID string) {
	if e.Session == "" {
		return VerdictNew, ""
	}
	for _, t := range tail {
		if t.Session == e.Session && t.ID == e.ID {
			return VerdictReplay, ""
		}
	}
	at, err := time.Parse(time.RFC3339, e.At)
	if err != nil {
		return VerdictNew, ""
	}
	input := e.Input.String()
	for i := len(tail) - 1; i >= 0; i-- {
		t := tail[i]
		if t.Dup != "" || t.Session != e.Session || t.Tool != e.Tool || t.Kind != e.Kind ||
			t.FP != e.FP || t.Input != input {
			continue
		}
		tat, err := time.Parse(time.RFC3339, t.At)
		if err != nil {
			continue
		}
		if gap := at.Sub(tat); gap < 0 || gap > repeatWindow {
			continue
		}
		return VerdictRepeat, t.ID
	}
	return VerdictNew, ""
}
