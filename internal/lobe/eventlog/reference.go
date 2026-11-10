package eventlog

import "github.com/tentaqles/tentaqles/internal/lobe/scrub"

// Reference is the event a reference line stands for: the fields that say "this happened again"
// and point at the first line (its id), with no input, error, exit, repo or cwd. Dup is firstID.
func Reference(e Event, firstID string) Event {
	return Event{
		V:       e.V,
		ID:      e.ID,
		At:      e.At,
		Session: e.Session,
		Host:    e.Host,
		Tool:    e.Tool,
		Kind:    e.Kind,
		FP:      e.FP,
		Dup:     firstID,
	}
}

// referenceLine is the shape of a reference line on disk: exactly these keys, fp only when set.
type referenceLine struct {
	V       int    `json:"v"`
	ID      string `json:"id"`
	At      string `json:"at"`
	Session string `json:"session"`
	Host    string `json:"host"`
	Tool    string `json:"tool"`
	Kind    string `json:"kind"`
	FP      string `json:"fp,omitempty"`
	Dup     string `json:"dup"`
}

// ReferenceLine marshals r, a value from Reference, to one line with the keys v, id, at, session,
// host, tool, kind, fp (when set) and dup. Event itself is not used: its cwd has no omitempty.
func ReferenceLine(r Event) (scrub.Clean, error) {
	return scrub.Line(referenceLine{
		V:       r.V,
		ID:      r.ID,
		At:      r.At,
		Session: r.Session,
		Host:    r.Host,
		Tool:    r.Tool,
		Kind:    r.Kind,
		FP:      r.FP,
		Dup:     r.Dup,
	})
}
