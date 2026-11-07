// Package audit appends identity-switch events to ~/.tentaqles/audit.jsonl
// and every denial (a refused human-only command, a hook deny of any
// category) to ~/.tentaqles/denials.jsonl.
package audit

import (
	"bufio"
	"encoding/json"
	"io"
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
	// Layer is where it was caught: hook, command (a command's own refusal), filesystem (a protected path), or
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
	// LayerCommand is a tq command refusing its own request, outside the hook (tentaqles install or
	// tentaqles connect, for a provider outside the identity's scope).
	LayerCommand = "command"
)

// Callers a Denial names.
const (
	CallerHarness  = "harness"
	CallerSubagent = "subagent"
	CallerNoTTY    = "no-tty"
	// CallerHuman names a refusal of a tq command that is not human-only, here the scope refusal
	// of tentaqles install, made for a person at a terminal (the others name an agent or no tty).
	CallerHuman = "human"
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
	return appendDenialLine(DenialsPath(), d)
}

// MaxDenialLog is the size at which the denial log rotates: the current
// file becomes denials.jsonl.1 (replacing the one before) and a new file
// starts, so the log holds at most about twice this.
var MaxDenialLog int64 = 1 << 20

// appendDenialLine rotates the log when the line would take it past
// MaxDenialLog, then appends. A rotation that fails (another hook rotated
// first) changes nothing: the line is appended all the same.
func appendDenialLine(path string, d Denial) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size()+int64(len(b))+1 > MaxDenialLog {
		_ = os.Rename(path, path+".1")
	}
	return appendBytes(path, append(b, '\n'))
}

// EachDenial calls fn for every well-formed line of the denial log, oldest
// first (the rotated denials.jsonl.1, then denials.jsonl), reading one line
// at a time; it stops when fn returns false. A line that does not parse, or
// is longer than a denial can be, is skipped. A missing file is no denials.
func EachDenial(fn func(Denial) bool) error {
	for _, path := range []string{DenialsPath() + ".1", DenialsPath()} {
		more, err := eachDenialIn(path, fn)
		if err != nil || !more {
			return err
		}
	}
	return nil
}

func eachDenialIn(path string, fn func(Denial) bool) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := readLine(r, 64<<10)
		if len(line) > 0 {
			if d, ok := parseDenial(line); ok && !fn(d) {
				return false, nil
			}
		}
		if err == io.EOF {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
}

// readLine reads one line, dropping (and returning empty) a line longer
// than limit so one runaway line cannot make the reader allocate without
// bound.
func readLine(r *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	tooLong := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(chunk) > limit {
				tooLong, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if tooLong {
			return nil, err
		}
		return buf, err
	}
}

func parseDenial(raw []byte) (Denial, bool) {
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return Denial{}, false
	}
	var d Denial
	if json.Unmarshal([]byte(line), &d) != nil {
		return Denial{}, false
	}
	if d.Category == "" {
		// A line written before ADR-0006 named the category kind and the
		// identity workspace.
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
	return d, true
}

// ReadDenials returns every well-formed line of the denial log, oldest
// first (EachDenial).
func ReadDenials() ([]Denial, error) {
	var out []Denial
	err := EachDenial(func(d Denial) bool { out = append(out, d); return true })
	return out, err
}

func appendLine(path string, v any) error {
	b, _ := json.Marshal(v)
	return appendBytes(path, append(b, '\n'))
}

func appendBytes(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(b)
	return err
}
