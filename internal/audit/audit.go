// Package audit appends identity-switch events to ~/.tentaqles/audit.jsonl.
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
	if err := os.MkdirAll(filepath.Dir(paths.Audit()), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(paths.Audit(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(e)
	_, err = f.Write(append(b, '\n'))
	return err
}
