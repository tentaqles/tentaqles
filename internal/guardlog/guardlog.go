// Package guardlog records the confirms and blocks unattended mode skipped,
// masked, in $TQ_HOME/guard/unattended.jsonl. Writes never fail the caller.
package guardlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/redact"
)

const (
	rotateAt   = 5 << 20
	maxCommand = 200
)

type Record struct {
	Time  time.Time `json:"ts"`
	WS    string    `json:"ws"`
	Scope string    `json:"scope"`
	// Mode is the tq guard mode (default|relaxed|strict|custom) the call ran
	// under. Records written before spec E have none.
	Mode string `json:"tq_mode,omitempty"`
	// ClaudeMode is Claude Code's permission mode. Records written before
	// spec E stored it as "mode"; UnmarshalJSON reads that key into it.
	ClaudeMode string `json:"claude_mode,omitempty"`
	// To is the action applied to the skipped or lowered rules: "allow"
	// (skipped) or a lower one ("ask" for a lowered deny). Empty: allow.
	To string `json:"to,omitempty"`
	// Action is the strictest verdict skipped: "deny" (a block) or "ask" (a
	// confirm). Records written before denies could be skipped have none:
	// they are confirms.
	Action  string   `json:"action,omitempty"`
	Rules   []string `json:"rule_ids"`
	Tool    string   `json:"tool"`
	Command string   `json:"command"`
}

// UnmarshalJSON reads a record, mapping the legacy "mode" key (Claude's
// permission mode, before spec E) to ClaudeMode.
func (r *Record) UnmarshalJSON(b []byte) error {
	type plain Record
	var aux struct {
		plain
		Legacy string `json:"mode"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*r = Record(aux.plain)
	if r.ClaudeMode == "" {
		r.ClaudeMode = aux.Legacy
	}
	return nil
}

// IsBlock reports whether r records a skipped deny.
func (r Record) IsBlock() bool { return r.Action == "deny" }

func Path() string { return filepath.Join(paths.Home(), "guard", "unattended.jsonl") }

// Append masks r.Command with classes, truncates it and appends r.
func Append(r Record, classes []string) {
	if r.Time.IsZero() {
		r.Time = time.Now().UTC()
	}
	c := []rune(redact.Redact(r.Command, classes))
	if len(c) > maxCommand {
		c = append(c[:maxCommand-1], '…')
	}
	r.Command = string(c)
	p := Path()
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	if fi, err := os.Stat(p); err == nil && fi.Size() >= rotateAt {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(r)
	_, _ = f.Write(append(b, '\n'))
}

// Read returns ws's records since since, newest first, from the current
// file only. maxBytes > 0 reads only the file's tail (the hook path stays
// bounded); ws "" means every workspace.
func Read(ws string, since time.Time, maxBytes int64) []Record {
	return newestFirst(readFile(Path(), ws, since, maxBytes))
}

// ReadAll is Read over the whole rotated file (unattended.jsonl.1) and the
// whole current file. It is for the CLI, never for a hook.
func ReadAll(ws string, since time.Time) []Record {
	p := Path()
	return newestFirst(append(readFile(p+".1", ws, since, 0), readFile(p, ws, since, 0)...))
}

func newestFirst(out []Record) []Record {
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// readFile returns p's matching records in file order.
func readFile(p, ws string, since time.Time, maxBytes int64) []Record {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var src io.Reader = f
	if fi, err := f.Stat(); err == nil && maxBytes > 0 && fi.Size() > maxBytes {
		if _, err := f.Seek(fi.Size()-maxBytes, io.SeekStart); err == nil {
			br := bufio.NewReader(f)
			_, _ = br.ReadBytes('\n') // drop the partial first line
			src = br
		}
	}
	var out []Record
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(bytes.TrimSpace(sc.Bytes()), &r) != nil {
			continue
		}
		if (ws == "" || r.WS == ws) && !r.Time.Before(since) {
			out = append(out, r)
		}
	}
	return out
}
