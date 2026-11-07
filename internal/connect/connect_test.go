package connect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var httpEntry = map[string]any{"type": "http", "url": "https://mcp.example.test/mcp"}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// F-02: a damaged config is named and left as it is; a missing one is created.
func TestConnect_DamagedClaudeJson(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".claude.json")
	for name, body := range map[string]string{
		"invalid json":              `{"mcpServers": {`,
		"trailing garbage":          `{"a":1} x`,
		"mcpServers is a string":    `{"mcpServers": "none", "k": 1}`,
		"mcpServers is an array":    `{"mcpServers": []}`,
		"mcpServers is null":        `{"mcpServers": null}`,
		"top level is an array":     `[1,2]`,
		"top level is a scalar":     `"x"`,
		"unquoted key":              `{mcpServers: {}}`,
		"byte order mark":           "\ufeff{}",
		"truncated string in value": `{"a": "abc`,
	} {
		write(t, p, body)
		added, err := Add(p, "github", httpEntry, nil)
		if err == nil || added {
			t.Errorf("%s: added=%v err=%v, want an error", name, added, err)
			continue
		}
		if !strings.Contains(err.Error(), p) {
			t.Errorf("%s: the error does not name the file: %v", name, err)
		}
		if got := read(t, p); string(got) != body {
			t.Errorf("%s: the file changed: %q", name, got)
		}
	}
	if _, err := os.Stat(p + ".tq-connect.lock"); err == nil {
		t.Error("the lock file is left behind")
	}

	fresh := filepath.Join(dir, "new", ".claude.json")
	added, err := Add(fresh, "github", httpEntry, nil)
	if err != nil || !added {
		t.Fatalf("missing file: added=%v err=%v", added, err)
	}
	var got struct {
		MCP map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(read(t, fresh), &got); err != nil || got.MCP["github"]["url"] != "https://mcp.example.test/mcp" {
		t.Fatalf("created file: %v %s", err, read(t, fresh))
	}
	empty := filepath.Join(dir, "empty.json")
	write(t, empty, " \n")
	if added, err := Add(empty, "github", httpEntry, nil); err != nil || !added {
		t.Fatalf("empty file: added=%v err=%v", added, err)
	}
}

// Every other byte stays as Claude Code wrote it.
func TestConnect_KeepsEveryOtherKey(t *testing.T) {
	entry := `{"type":"http","url":"https://mcp.example.test/mcp"}`
	cases := []struct{ name, before, after string }{
		{"into an existing list",
			"{\n  \"numStartups\": 12,\n  \"big\": 12345678901234567890,\n  \"mcpServers\": {\n    \"mydb\": {\"command\": \"x\"}\n  },\n  \"projects\": {\"/p\": {\"mcpServers\": {\"github\": {\"command\": \"mine\"}}}}\n}\n",
			"{\n  \"numStartups\": 12,\n  \"big\": 12345678901234567890,\n  \"mcpServers\": {\n    \"mydb\": {\"command\": \"x\"},\n    \"github\": " + entry + "\n  },\n  \"projects\": {\"/p\": {\"mcpServers\": {\"github\": {\"command\": \"mine\"}}}}\n}\n"},
		{"an empty list", `{"a": 1, "mcpServers": {}}`, `{"a": 1, "mcpServers": {"github": ` + entry + `}}`},
		{"no list", "{\n  \"a\": 1\n}\n", "{\n  \"a\": 1,\n  \"mcpServers\": {\"github\": " + entry + "}\n}\n"},
		{"an empty object", `{}`, `{"mcpServers": {"github": ` + entry + `}}`},
		{"a key in a string", `{"s": "\"mcpServers\": {", "mcpServers": {"x": {}}}`, `{"s": "\"mcpServers\": {", "mcpServers": {"x": {},"github": ` + entry + `}}`},
	}
	for _, c := range cases {
		p := filepath.Join(t.TempDir(), ".claude.json")
		write(t, p, c.before)
		added, err := Add(p, "github", httpEntry, nil)
		if err != nil || !added {
			t.Errorf("%s: added=%v err=%v", c.name, added, err)
			continue
		}
		if got := string(read(t, p)); got != c.after {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.after)
		}
	}
}

func TestConnect_AlreadyThereWritesNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	body := `{"mcpServers": {"github": {"command": "a-users-own"}}}`
	write(t, p, body)
	logged := false
	added, err := Add(p, "github", httpEntry, func(string) error { logged = true; return nil })
	if err != nil || added || logged {
		t.Fatalf("added=%v logged=%v err=%v", added, logged, err)
	}
	if string(read(t, p)) != body {
		t.Fatal("the file changed")
	}
}

func TestConnect_FailedBeforeStopsTheWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	body := `{"a":1}`
	write(t, p, body)
	if _, err := Add(p, "github", httpEntry, func(string) error { return fmt.Errorf("log full") }); err == nil {
		t.Fatal("want the log error")
	}
	if string(read(t, p)) != body {
		t.Fatal("written without its log line")
	}
}

// Q-01: eight runs at once leave valid JSON holding all eight.
func TestConnect_AtomicWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".claude.json")
	write(t, p, "{\n  \"keep\": [1, 2, 3]\n}\n")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Add(p, fmt.Sprintf("server-%d", i), map[string]any{"type": "http", "url": fmt.Sprintf("https://s%d.example.test/", i)}, nil)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	b := read(t, p)
	var got struct {
		Keep []int                     `json:"keep"`
		MCP  map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, b)
	}
	if len(got.MCP) != 8 || len(got.Keep) != 3 {
		t.Fatalf("want 8 servers and the other key, got %d and %v:\n%s", len(got.MCP), got.Keep, b)
	}
	left, _ := filepath.Glob(p + "*")
	if len(left) != 1 {
		t.Fatalf("temporary or lock files left behind: %v", left)
	}
}

func TestLog_NoValueOfTheEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", home)
	if LogPath() != filepath.Join(home, LogFile) {
		t.Fatalf("the log would not go to the test's home: %s", LogPath())
	}
	if err := Log(Record{Identity: "acme", Provider: "gh", Server: "github", Caller: CallerHarness}); err != nil {
		t.Fatal(err)
	}
	b := read(t, LogPath())
	if bytes.Count(b, []byte("\n")) != 1 || strings.Contains(string(b), "http") {
		t.Fatalf("log line: %q", b)
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil || r.Time.IsZero() || r.Identity != "acme" || r.Provider != "gh" || r.Server != "github" || r.Caller != "harness" {
		t.Fatalf("record: %+v %v", r, err)
	}
}
