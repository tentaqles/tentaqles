package sense

import (
	"context"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// Outcome is what recording a call gave, for the steps that follow in the same process.
type Outcome struct {
	Session string
	ID      string
	Tool    string
	CallID  string
	Call    string // CallText of the call
	Kind    string // eventlog.KindFail here
	FP      string // empty for now
	State   string // StateFull or StateDropped
}

const (
	StateFull    = "full"    // a full line was written
	StateDropped = "dropped" // the line could not be written; Kind is still set
)

// Record writes the event to the client's log and returns the outcome. When the write fails the
// outcome's State is StateDropped and the error is returned too, for the caller to classify.
func Record(ctx context.Context, s *store.Store, e eventlog.Event, c Call) (Outcome, error) {
	out := Outcome{
		Session: e.Session,
		ID:      e.ID,
		Tool:    e.Tool,
		CallID:  c.CallID,
		Call:    CallText(e.Tool, c.Input),
		Kind:    e.Kind,
		State:   StateFull,
	}
	if err := eventlog.Append(ctx, s, e); err != nil {
		out.State = StateDropped
		return out, err
	}
	return out, nil
}
