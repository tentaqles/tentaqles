// Package eventlog holds the shape, the id and the append of one event of a client's event log:
// one JSON object per line, in one file per UTC day, events/events-YYYYMMDD.jsonl, inside the
// client's lobe store.
package eventlog

import "github.com/tentaqles/tentaqles/internal/lobe/scrub"

// Event is one line of a client's event log.
type Event struct {
	V       int         `json:"v"`
	ID      string      `json:"id"`
	At      string      `json:"at"`
	Session string      `json:"session"`
	Host    string      `json:"host"`
	Tool    string      `json:"tool"`
	Kind    string      `json:"kind"`
	Repo    string      `json:"repo,omitempty"`
	Cwd     string      `json:"cwd"`
	Input   scrub.Clean `json:"input,omitzero"`
	Error   scrub.Clean `json:"error,omitzero"`
	Exit    *int        `json:"exit,omitempty"`
	FP      string      `json:"fp,omitempty"`
	Dup     string      `json:"dup,omitempty"`
}

const (
	KindFail       = "fail"
	KindOK         = "ok"
	KindHiddenFail = "hidden-fail"
)
