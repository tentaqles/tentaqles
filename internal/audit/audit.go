// Package audit appends identity-switch events to ~/.tentaqles/audit.jsonl
// and every denial (a refused human-only command, a hook deny of any
// category) to ~/.tentaqles/denials.jsonl.
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

// Denial is one refused action: what was asked, which rule or detector
// refused it and where. Kind is the guard category (identity, secrets,
// self-protection) for a hook deny, human-only for a refused command;
// Layer says which layer caught it (hook, command). It carries no
// environment value and no file content: Command is the command line or
// the path, never what was read or written.
type Denial struct {
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"`
	Layer     string    `json:"layer,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	Command   string    `json:"command"`
	Rule      string    `json:"rule,omitempty"`
	Reason    string    `json:"reason"`
	Workspace string    `json:"workspace,omitempty"`
	PID       int       `json:"pid"`
	Cwd       string    `json:"cwd,omitempty"`
}

// Layers a Denial names.
const (
	LayerHook    = "hook"
	LayerCommand = "command"
)

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
