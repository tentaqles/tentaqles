// Package audit appends identity-switch events to ~/.tentaqles/audit.jsonl
// and every denial (a refused human-only command, a hook deny of any
// category) to ~/.tentaqles/denials.jsonl.
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// Denial is one refused action (PRD v2 §6.1 step 5, §7.3; ADR-0006): who
// asked (Identity, Caller, Harness), what (Tool, Command, Subject), which
// rule refused it (Category, Rule, Reason) and which layer caught it
// (Layer). It carries no environment value and no file content.
//
// Command is the call as RedactCommand leaves it (the hook applies it):
// the shell line with here-doc bodies, quoted string bodies, assignment and
// credential-flag values, secret-shaped spans and long tokens masked, cut
// to MaxCommandRunes; for a file tool the path; for an MCP call or a patch
// the tool name. Subject is the one path or token the denial named (the
// matched protected path, the human-only tq verb). For a human-only refusal
// (internal/humanonly) Command is the command name.
type Denial struct {
	Time time.Time `json:"time"`
	// Category is the guard category (identity, secrets, self-protection),
	// or tq/guard-error for a call the guard could not judge.
	Category string `json:"category"`
	// Layer is where it was caught: hook, filesystem (a protected path), or
	// human-only (the command's own refusal).
	Layer string `json:"layer"`
	// Caller is the calling process: harness (the main session), subagent
	// (the payload names an agent), or for a human-only refusal what the
	// detector found (no-tty, harness).
	Caller string `json:"caller,omitempty"`
	// Harness is the agent the hook answered (claude, codex).
	Harness  string `json:"harness,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Command  string `json:"command"`
	Subject  string `json:"subject,omitempty"`
	Rule     string `json:"rule,omitempty"`
	Reason   string `json:"reason"`
	Identity string `json:"identity,omitempty"`
	PID      int    `json:"pid"`
	Cwd      string `json:"cwd,omitempty"`
}

// Layers a Denial names.
const (
	LayerHook       = "hook"
	LayerFilesystem = "filesystem"
	LayerHumanOnly  = "human-only"
)

// Callers a Denial names.
const (
	CallerHarness  = "harness"
	CallerSubagent = "subagent"
	CallerNoTTY    = "no-tty"
)

// DenialsPath is the denial log, <tq home>/denials.jsonl.
func DenialsPath() string { return filepath.Join(paths.Home(), "denials.jsonl") }

// AppendDenial appends one JSON line to the denial log. Whatever the caller
// passed, every text field has its secret-shaped spans masked and is cut
// (RedactText); a shell command line must already have gone through
// RedactCommand, which the hook does.
func AppendDenial(d Denial) error {
	if d.Time.IsZero() {
		d.Time = time.Now()
	}
	d.Command = truncate(RedactText(d.Command), MaxCommandRunes)
	d.Subject = RedactText(d.Subject)
	d.Reason = RedactText(d.Reason)
	return appendLine(DenialsPath(), d)
}

// ReadDenials returns every well-formed line of the denial log, oldest
// first; a line that does not parse is skipped. A missing log is no
// denials.
func ReadDenials() ([]Denial, error) {
	raw, err := os.ReadFile(DenialsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Denial
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var d Denial
		if json.Unmarshal([]byte(line), &d) != nil {
			continue
		}
		if d.Category == "" {
			// A line written before ADR-0006 named the category kind and
			// the identity workspace.
			var old struct {
				Kind      string `json:"kind"`
				Workspace string `json:"workspace"`
			}
			_ = json.Unmarshal([]byte(line), &old)
			d.Category, d.Identity = old.Kind, old.Workspace
			if d.Category == "human-only" {
				d.Category, d.Layer = "self-protection", LayerHumanOnly
			}
		}
		out = append(out, d)
	}
	return out, nil
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
