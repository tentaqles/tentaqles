// Package audit appends identity-switch events to ~/.tentaqles/audit.jsonl
// and refused human-only commands to ~/.tentaqles/denials.jsonl.
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

type Event struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	From   string    `json:"from,omitempty"`
	To     string    `json:"to,omitempty"`
	Cwd    string    `json:"cwd,omitempty"`
	Reason string    `json:"reason,omitempty"`
}

func Append(e Event) error {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	return appendLine(paths.Audit(), e)
}

// Denial is one refused command: what was asked, which detector refused it
// and where. It carries no environment value.
type Denial struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	Command string    `json:"command"`
	Reason  string    `json:"reason"`
	PID     int       `json:"pid"`
	Cwd     string    `json:"cwd,omitempty"`
}

// DenialsPath is the denial log, <tq home>/denials.jsonl.
func DenialsPath() string { return filepath.Join(paths.Home(), "denials.jsonl") }

// AppendDenial appends one JSON line to the denial log.
func AppendDenial(d Denial) error {
	if d.Time.IsZero() {
		d.Time = time.Now()
	}
	return appendLine(DenialsPath(), d)
}

func appendLine(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(v)
	_, err = f.Write(append(b, '\n'))
	return err
}
