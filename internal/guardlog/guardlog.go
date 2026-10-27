// Package guardlog records the confirms unattended mode skipped, masked, in
// $TQ_HOME/guard/unattended.jsonl. Writes never fail the caller.
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
	Time    time.Time `json:"ts"`
	WS      string    `json:"ws"`
	Scope   string    `json:"scope"`
	Mode    string    `json:"mode"`
	Rules   []string  `json:"rule_ids"`
	Tool    string    `json:"tool"`
	Command string    `json:"command"`
}

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

// Read returns ws's records since since, newest first. maxBytes > 0 reads
// only the file's tail; ws "" means every workspace.
func Read(ws string, since time.Time, maxBytes int64) []Record {
	f, err := os.Open(Path())
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
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
