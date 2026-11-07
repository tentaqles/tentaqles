package connect

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// L-02: a write by Claude Code between connect's read and its rename is not lost: the edit is
// made again from the new bytes, once per change, and the log line is written once.
func TestConnect_ConcurrentWriterIsKept(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	write(t, p, `{"numStartups": 1}`)
	writes := 0
	betweenReadAndRename = func() {
		if writes < 2 {
			writes++
			write(t, p, `{"numStartups": `+string(rune('1'+writes))+`, "late": true}`)
		}
	}
	t.Cleanup(func() { betweenReadAndRename = nil })
	var outcomes []string
	added, err := Add(p, "github", httpEntry, func(o string) error { outcomes = append(outcomes, o); return nil })
	if err != nil || !added || len(outcomes) != 1 || outcomes[0] != OutcomeConnected {
		t.Fatalf("added=%v outcomes=%v err=%v", added, outcomes, err)
	}
	var got struct {
		N    float64                   `json:"numStartups"`
		Late bool                      `json:"late"`
		MCP  map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(read(t, p), &got); err != nil {
		t.Fatal(err)
	}
	if got.N != 3 || !got.Late || got.MCP["github"]["url"] != httpEntry["url"] {
		t.Fatalf("a concurrent write was lost: %s", read(t, p))
	}
}

// L-08: a file that never settles is not overwritten, and no change is logged: the log is
// written only for a change made, immediately before the replacement.
func TestConnect_NeverSettlingFileIsLeftAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	write(t, p, `{"n": 0}`)
	n := 0
	betweenReadAndRename = func() { n++; write(t, p, `{"n": `+strings.Repeat("1", n)+`}`) }
	t.Cleanup(func() { betweenReadAndRename = nil })
	logged := 0
	added, err := Add(p, "github", httpEntry, func(string) error { logged++; return nil })
	if err == nil || added {
		t.Fatalf("added=%v err=%v, want an error", added, err)
	}
	if logged != 0 {
		t.Fatalf("%d log lines for a change that was never made", logged)
	}
	if strings.Contains(string(read(t, p)), "mcpServers") {
		t.Fatal("wrote over a file that kept changing")
	}
}

// L-08: a retry that finds the server already added changes nothing and logs nothing.
func TestConnect_RetryFindingItDoneLogsNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	write(t, p, `{"n": 0}`)
	betweenReadAndRename = func() {
		write(t, p, `{"n": 1, "mcpServers": {"github": {"type": "http", "url": "https://other.test/"}}}`)
	}
	t.Cleanup(func() { betweenReadAndRename = nil })
	logged := 0
	added, err := Add(p, "github", httpEntry, func(string) error { logged++; return nil })
	if err != nil || added || logged != 0 {
		t.Fatalf("added=%v logged=%d err=%v, want no change and no line", added, logged, err)
	}
}

// L-08: a replacement that fails after its connected line is recorded as failed, and the file is
// left as it was.
func TestConnect_FailedRenameIsRecorded(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	body := `{"n": 0}`
	write(t, p, body)
	prev := renameFile
	renameFile = func(string, string) error { return errors.New("access denied") }
	t.Cleanup(func() { renameFile = prev })
	var outcomes []string
	added, err := Add(p, "github", httpEntry, func(o string) error { outcomes = append(outcomes, o); return nil })
	if err == nil || added {
		t.Fatalf("added=%v err=%v, want the rename error", added, err)
	}
	if strings.Join(outcomes, ",") != OutcomeConnected+","+OutcomeFailed {
		t.Fatalf("outcomes = %v, want connected then failed", outcomes)
	}
	if string(read(t, p)) != body {
		t.Fatal("the file changed although the rename failed")
	}
}
