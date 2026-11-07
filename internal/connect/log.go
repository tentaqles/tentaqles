package connect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// LogFile is the connection log, <tq home>/connections.jsonl: one line per change connect made.
const LogFile = "connections.jsonl"

// Callers a Record names.
const (
	CallerHuman   = "human"
	CallerHarness = "harness"
)

// Record is one change: when, for which identity, which provider, which server name and who
// ran it. It carries no part of the entry written (no URL, command or argument).
type Record struct {
	Time     time.Time `json:"time"`
	Identity string    `json:"identity"`
	Provider string    `json:"provider"`
	Server   string    `json:"server"`
	Caller   string    `json:"caller"`
	// Outcome is OutcomeConnected for a change made, or OutcomeFailed when the replacement that
	// followed a connected line did not happen. A connected line with no failed line after it is
	// the change.
	Outcome string `json:"outcome"`
}

// LogPath is the connection log.
func LogPath() string { return filepath.Join(paths.Home(), LogFile) }

// Log appends r as one JSON line (the time defaults to now).
func Log(r Record) error {
	if r.Time.IsZero() {
		r.Time = time.Now().UTC()
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	p := LogPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}
