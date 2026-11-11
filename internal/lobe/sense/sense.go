package sense

import (
	"context"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/fingerprint"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// Outcome is what recording a call gave, for the steps that follow in the same process.
type Outcome struct {
	Session string
	ID      string
	Tool    string
	CallID  string
	Call    string // CallText of the call
	Kind    string // eventlog.KindFail or KindHiddenFail for a failure, else KindOK
	FP      string // the fingerprint of a fail or hidden-fail event (S-08); empty for ok
	State   string // StateFull, StateRef, StateReplay or StateDropped
}

const (
	StateFull    = "full"    // a full line was written
	StateRef     = "ref"     // a reference line was written: a repeat of a full line (S-06)
	StateReplay  = "replay"  // the line was already in the log; nothing was written (S-06)
	StateDropped = "dropped" // the line could not be written; Kind is still set
)

// Record writes the event to the client's log and returns the outcome. When the event is dropped (a limit
// of S-07 refused it, or the write failed) the outcome's State is StateDropped; a write failure also
// returns its error for the caller to classify. A limit refusal is not an error: the drop is counted in
// the drops file and nothing is returned.
func Record(ctx context.Context, s *store.Store, e eventlog.Event, c Call) (Outcome, error) {
	// S-08: a failure gets its fingerprint from the event's own tool and scrubbed error, between the
	// scrubbing (S-05, done when the event was built) and the append (S-07), so a reader can work the same
	// fp out again from the stored line. An ok line has none; a hidden-fail line has one, as a fail does.
	if e.Kind == eventlog.KindFail || e.Kind == eventlog.KindHiddenFail {
		e.FP, _ = fingerprint.Of(e.Tool, e.Error.String())
	}
	out := Outcome{
		FP:      e.FP,
		Session: e.Session,
		ID:      e.ID,
		Tool:    e.Tool,
		CallID:  c.CallID,
		Call:    CallText(e.Tool, c.Input),
		Kind:    e.Kind,
		State:   StateFull,
	}
	v, _, err := eventlog.AppendChecked(ctx, s, e)
	if err != nil {
		out.State = StateDropped
		return out, err
	}
	switch v {
	case eventlog.VerdictDropped:
		out.State = StateDropped
	case eventlog.VerdictRepeat:
		out.State = StateRef
	case eventlog.VerdictReplay:
		out.State = StateReplay
	}
	return out, nil
}
