package connect

import (
	"encoding/json"
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
	logged := 0
	added, err := Add(p, "github", httpEntry, func() error { logged++; return nil })
	if err != nil || !added || logged != 1 {
		t.Fatalf("added=%v logged=%d err=%v", added, logged, err)
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

// A file that never settles is not overwritten.
func TestConnect_NeverSettlingFileIsLeftAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	write(t, p, `{"n": 0}`)
	n := 0
	betweenReadAndRename = func() { n++; write(t, p, `{"n": `+strings.Repeat("1", n)+`}`) }
	t.Cleanup(func() { betweenReadAndRename = nil })
	added, err := Add(p, "github", httpEntry, nil)
	if err == nil || added {
		t.Fatalf("added=%v err=%v, want an error", added, err)
	}
	if strings.Contains(string(read(t, p)), "mcpServers") {
		t.Fatal("wrote over a file that kept changing")
	}
}
