package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

type fakeClaude struct {
	bin     []string
	dir     string
	argvLog string
}

func newFakeClaude(t *testing.T, rounds []map[string]any) fakeClaude {
	t.Helper()
	isolate(t)
	py := testutil.Python(t)
	dir := t.TempDir()
	scriptPath, err := filepath.Abs(filepath.Join("testdata", "fake_claude.py")) // the round runs in dir, not here
	if err != nil {
		t.Fatal(err)
	}
	f := fakeClaude{bin: append(py, scriptPath), dir: dir, argvLog: filepath.Join(dir, "argv.jsonl")}
	script := map[string]any{"counter": filepath.Join(dir, "counter"), "argv_log": f.argvLog, "rounds": rounds}
	raw, _ := json.Marshal(script)
	p := filepath.Join(dir, "script.json")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CLAUDE_SCRIPT", p)
	return f
}

func (f fakeClaude) calls(t *testing.T) []map[string]any {
	t.Helper()
	fh, err := os.Open(f.argvLog)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	var out []map[string]any
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		var m map[string]any
		json.Unmarshal(sc.Bytes(), &m)
		out = append(out, m)
	}
	return out
}

func run(t *testing.T, c Claude, req RunRequest) (RunResult, string) {
	t.Helper()
	var log syncBuf
	res, err := c.Start(context.Background(), req, &log)
	if err != nil {
		t.Fatal(err)
	}
	return res, log.String()
}

func TestClaudeArgvAndPermissionWrapper(t *testing.T) {
	isolate(t)
	c := Claude{Bin: []string{"claude"}, Args: func(a []string) []string { return append([]string{"--permission-mode", "acceptEdits"}, a...) }}
	got := c.Argv(RunRequest{Prompt: "/tentaqles:feature x --headless & echo pwned", SessionID: "s1", AllowedTools: []string{"Bash(npm test)", "Edit"}})
	want := []string{"claude", "--permission-mode", "acceptEdits", "-p", "--output-format", "json", "--resume=s1"}
	if !slices.Equal(got, want) {
		t.Fatalf("argv\n got %q\nwant %q", got, want)
	}
	for _, a := range got {
		if strings.Contains(a, "pwned") || strings.Contains(a, "npm test") || a == "--allowedTools" {
			t.Fatalf("free text on argv: %q", got)
		}
	}
	plain := Claude{}.Argv(RunRequest{Prompt: "p"})
	if !slices.Equal(plain, []string{"claude", "-p", "--output-format", "json"}) || slices.Contains(plain, "--dangerously-skip-permissions") {
		t.Fatalf("plain argv %q", plain)
	}
	for _, bad := range []string{"s1 & calc", "a b", "x\ny", "s1\"", "%PATH%", "-x", "--dangerously-skip-permissions", "-p", ".hidden"} {
		for _, a := range (Claude{}).Argv(RunRequest{Prompt: "p", SessionID: bad}) {
			if strings.HasPrefix(a, "--resume") || (bad != "-p" && strings.Contains(a, bad)) {
				t.Fatalf("malformed session id %q reached argv: %q", bad, a)
			}
		}
	}
	if n := countPrefix(Claude{}.Argv(RunRequest{Prompt: "p", SessionID: "s-1.a_b"}), "--resume"); n != 1 {
		t.Fatalf("a valid id yields exactly one element, got %d", n)
	}
	if !slices.Contains(Claude{}.Argv(RunRequest{Prompt: "p", SessionID: "s-1.a_b"}), "--resume=s-1.a_b") {
		t.Fatal("--resume=<id> is one token")
	}
}

func countPrefix(argv []string, prefix string) int {
	n := 0
	for _, a := range argv {
		if strings.HasPrefix(a, prefix) {
			n++
		}
	}
	return n
}

func TestClaudeMalformedSessionLogged(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{{"result": "ok"}})
	_, log := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", SessionID: "bad id & calc", Env: os.Environ()})
	if !strings.Contains(log, "ignored malformed session id") {
		t.Fatalf("log %q", log)
	}
	if argv, _ := f.calls(t)[0]["argv"].([]any); slices.ContainsFunc(argv, func(a any) bool { s, _ := a.(string); return strings.HasPrefix(s, "--resume") }) {
		t.Fatalf("argv %v", argv)
	}
}

func TestClaudePromptAndToolsRoundTripVerbatim(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{{"result": "ok"}})
	prompt := "do it & echo pwned | more > out.txt %PATH% ^x \"q\"\nsecond line"
	tools := []string{"Bash(x & echo pwned)", "Edit"}
	run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: prompt, AllowedTools: tools, Env: os.Environ()})
	call := f.calls(t)[0]
	if call["prompt"] != prompt {
		t.Fatalf("prompt %q", call["prompt"])
	}
	got, _ := call["allowed_tools"].([]any)
	if len(got) != 2 || got[0] != tools[0] || got[1] != tools[1] {
		t.Fatalf("tools %v", call["allowed_tools"])
	}
	for _, a := range call["argv"].([]any) {
		if s, _ := a.(string); strings.Contains(s, "pwned") {
			t.Fatalf("free text on argv: %v", call["argv"])
		}
	}
}

func TestParseResultIsError(t *testing.T) {
	isolate(t)
	r := ParseResult([]byte(`{"type":"result","subtype":"success","is_error":true,"result":"x"}`))
	if r.StopReason != "error" {
		t.Fatalf("%+v", r)
	}
	r = ParseResult([]byte(`{"type":"result","subtype":"error_max_turns","is_error":true}`))
	if r.StopReason != "error_max_turns" {
		t.Fatalf("%+v", r)
	}
	r = ParseResult([]byte(`{"type":"result","subtype":"success","is_error":false}`))
	if r.StopReason != "success" {
		t.Fatalf("%+v", r)
	}
}

func TestClaudeAdapterSuccess(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{{"result": "done\nall good", "session_id": "s-42", "cost": 0.75, "num_turns": 4,
		"usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "cache_creation_input_tokens": 5, "cache_read_input_tokens": 1000}}})
	res, log := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "/tentaqles:feature x --headless", AllowedTools: []string{"Bash(npm test)"}, Env: LaunchEnv(os.Environ(), `{"hub":"c-x"}`)})
	if res.ExitCode != 0 || res.SessionID != "s-42" || res.CostUSD != 0.75 || !res.CostKnown || res.StopReason != "success" || res.NumTurns != 4 {
		t.Fatalf("res %+v", res)
	}
	if res.Usage != (Usage{Input: 100, Output: 20, CacheRead: 1000, CacheWrite: 5}) || res.Transcript != "done\nall good" {
		t.Fatalf("usage %+v transcript %q", res.Usage, res.Transcript)
	}
	if !strings.Contains(log, "fake claude round 0") {
		t.Fatalf("stderr goes to the log: %q", log)
	}
	call := f.calls(t)[0]
	if call["prompt"] != "/tentaqles:feature x --headless" {
		t.Fatalf("the prompt arrives on stdin: %v", call["prompt"])
	}
	if tools, _ := call["allowed_tools"].([]any); len(tools) != 1 || tools[0] != "Bash(npm test)" {
		t.Fatalf("allowed tools arrive through --settings: %v", call["allowed_tools"])
	}
	if call["headless"] != "1" || call["headless_child"] != false || call["build_token"] != false || call["hub_task"] != `{"hub":"c-x"}` {
		t.Fatalf("environment seen by claude: %v", call)
	}
}

func TestClaudeAdapterCostFallback(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{{"result": "ok", "model": "claude-opus-5-5",
		"usage": map[string]any{"input_tokens": 1_000_000, "output_tokens": 100_000, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}}})
	res, _ := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	// opus-5-5 list price: $4 per MTok in, $20 per MTok out
	if !res.CostKnown || math.Abs(res.CostUSD-6.0) > 1e-9 || res.Model != "claude-opus-5-5" {
		t.Fatalf("cost %v known %v model %q", res.CostUSD, res.CostKnown, res.Model)
	}
}

func TestClaudeAdapterNoUsageNoCost(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{{"result": "ok"}})
	res, _ := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.CostKnown || res.UsageKnown || res.CostUSD != 0 || res.Usage != (Usage{}) {
		t.Fatalf("res %+v", res)
	}
	g := newFakeClaude(t, []map[string]any{{"result": "ok", "model": "some-unpriced-model", "usage": map[string]any{"input_tokens": 10}}})
	res, _ = run(t, Claude{Bin: g.bin}, RunRequest{Dir: g.dir, Prompt: "p", Env: os.Environ()})
	if res.CostKnown || !res.UsageKnown || res.Usage.Input != 10 {
		t.Fatalf("an unpriced model leaves the cost unknown: %+v", res)
	}
}

func TestClaudeAdapterErrorShapes(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{
		{"subtype": "error_max_turns", "result": "ran out", "cost": 0.1},
		{"subtype": "error_during_execution", "result": "Bash was not allowed", "exit": 1,
			"permission_denials": []any{map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "npm test"}}}},
		{"raw": "this is not json"},
	})
	res, _ := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.StopReason != "error_max_turns" || res.ExitCode != 0 {
		t.Fatalf("max turns %+v", res)
	}
	res, _ = run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.StopReason != "error_during_execution" || res.ExitCode != 1 || len(res.PermissionDenials) != 1 || ToolSpec(res.PermissionDenials[0]) != "Bash(npm test)" {
		t.Fatalf("denial %+v", res)
	}
	res, _ = run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.StopReason != "unparseable output" || !strings.Contains(res.Transcript, "this is not json") {
		t.Fatalf("raw %+v", res)
	}
}

func TestToolSpec(t *testing.T) {
	isolate(t)
	if got := ToolSpec(PermissionDenial{Tool: "WebFetch"}); got != "WebFetch" {
		t.Fatal(got)
	}
	if got := ToolSpec(PermissionDenial{Tool: "Bash", Input: map[string]any{"command": "  go test ./...  "}}); got != "Bash(go test ./...)" {
		t.Fatal(got)
	}
}
