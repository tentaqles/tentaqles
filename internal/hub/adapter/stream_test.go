package adapter

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func assistant(id string, usage map[string]any, parent any, blocks ...map[string]any) map[string]any {
	content := make([]any, len(blocks))
	for i, b := range blocks {
		content[i] = b
	}
	msg := map[string]any{"id": id, "model": "claude-opus-5-5", "role": "assistant", "content": content}
	if usage != nil {
		msg["usage"] = usage
	}
	return map[string]any{"type": "assistant", "message": msg, "parent_tool_use_id": parent}
}

func tokens(in, out int) map[string]any {
	return map[string]any{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}
}

func TestClaudeStreamWritesMaskedProgress(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 300)
	events := []map[string]any{
		{"type": "user", "message": map[string]any{"role": "user", "content": "a plain string content"}, "session_id": "s-7"},
		assistant("m1", tokens(10, 5), nil, map[string]any{"type": "text", "text": "Running the tests now.\nsecond line stays out"}),
		assistant("m1", tokens(10, 5), nil, map[string]any{"type": "tool_use", "id": "t1", "name": "Bash",
			"input": map[string]any{"command": "git config user.email tester@example.com\necho hidden"}}),
		assistant("m2", tokens(20, 5), nil, map[string]any{"type": "tool_use", "id": "t2", "name": "Edit",
			"input": map[string]any{"file_path": filepath.Join(dir, "internal", "x.go"), "old_string": "a", "new_string": "b"}}),
		assistant("m3", tokens(1, 1), nil, map[string]any{"type": "tool_use", "id": "t3", "name": "Task",
			"input": map[string]any{"subagent_type": "Explore", "description": "Find the exporter", "prompt": "long prompt text"}}),
		assistant("m4", tokens(1, 1), "t3", map[string]any{"type": "text", "text": long}),
	}
	f := newFakeClaude(t, []map[string]any{{"result": "done", "session_id": "s-7", "cost": 0.5, "events": events,
		"usage": map[string]any{"input_tokens": 32, "output_tokens": 12}}})
	res, log := run(t, Claude{Bin: f.bin, ProgressEvery: time.Nanosecond}, RunRequest{Dir: dir, Prompt: "p", Env: os.Environ(), Classes: []string{"all"}})
	for _, want := range []string{"→ session started", "→ Running the tests now.", "→ tool Bash: git config user.email [REDACTED:",
		"→ editing internal/x.go", "→ subagent Explore: Find the exporter", "  → " + strings.Repeat("x", 200) + "…",
		"→ spent so far this round: $", "→ result success, $0.50, 1 turns", "fake claude round 0"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log misses %q:\n%s", want, log)
		}
	}
	for _, bad := range []string{"tester@example.com", "second line stays out", "echo hidden", `"type"`, "long prompt text", strings.Repeat("x", 201)} {
		if strings.Contains(log, bad) {
			t.Fatalf("log holds %q:\n%s", bad, log)
		}
	}
	// the result event is what counts: its cost and usage, not the stream's running total
	if res.SessionID != "s-7" || res.CostUSD != 0.5 || !res.CostKnown || res.Usage.Input != 32 || res.Transcript != "done" || res.OverBudget != "" {
		t.Fatalf("res %+v", res)
	}
}

func TestClaudeStreamCountsAMessageOnce(t *testing.T) {
	isolate(t)
	s := newStream(&syncBuf{}, RunRequest{}, time.Hour, nil)
	u := `{"input_tokens":100,"output_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":1000}`
	for _, l := range []string{
		`{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"text","text":"a"}],"usage":` + u + `}}`,
		`{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"tool_use","name":"Read","input":{}}],"usage":` + u + `}}`,
		`{"type":"assistant","message":{"id":"m2","model":"claude-opus-5-5","content":[],"usage":` + u + `}}`,
	} {
		s.Write([]byte(l + "\n"))
	}
	if s.usage != (Usage{Input: 200, Output: 20, CacheRead: 2000, CacheWrite: 10}) || s.tokens() != 230 {
		t.Fatalf("usage %+v", s.usage)
	}
}

func TestClaudeStreamSplitWritesAndNoResult(t *testing.T) {
	isolate(t)
	var log syncBuf
	s := newStream(&log, RunRequest{}, time.Hour, nil)
	line := `{"type":"system","subtype":"init","session_id":"s-3","model":"claude-opus-5-5"}` + "\n" +
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1000000,"output_tokens":0}}}`
	for i := 0; i < len(line); i += 7 {
		s.Write([]byte(line[i:min(i+7, len(line))]))
	}
	s.flush()
	res := s.outcome()
	// no result event: the spend and session the stream showed are kept, priced at the init model
	if res.StopReason != "unparseable output" || res.SessionID != "s-3" || !res.UsageKnown || res.Usage.Input != 1_000_000 ||
		!res.CostKnown || math.Abs(res.CostUSD-4) > 1e-9 || !strings.Contains(log.String(), "→ hi") {
		t.Fatalf("res %+v log %q", res, log.String())
	}
}

func TestClaudeFallsBackToTheSingleDocument(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{
		{"single": true, "result": "old shape", "session_id": "s-1", "cost": 0.25, "num_turns": 2},
		{"no_result": true, "session_id": "s-2", "events": []map[string]any{assistant("m1", tokens(10, 5), nil, map[string]any{"type": "text", "text": "working"})}},
	})
	res, _ := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.StopReason != "success" || res.SessionID != "s-1" || res.CostUSD != 0.25 || res.Transcript != "old shape" || res.NumTurns != 2 {
		t.Fatalf("single document %+v", res)
	}
	res, log := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ()})
	if res.StopReason != "unparseable output" || res.SessionID != "s-2" || res.Usage.Input != 10 || !strings.Contains(log, "→ working") {
		t.Fatalf("no result %+v log %q", res, log)
	}
}

// breach runs a round that spends, then would keep going for two minutes unless stopped.
func breach(t *testing.T, limit Limit, events []map[string]any) (RunResult, string, int, time.Duration) {
	t.Helper()
	f := newFakeClaude(t, []map[string]any{{"session_id": "s-9", "model": "claude-opus-5-5", "events": events, "pause": 120, "result": "never"}})
	var log syncBuf
	start := time.Now()
	res, err := Claude{Bin: f.bin, Grace: time.Second}.Start(context.Background(), RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ(), Limit: limit}, &log)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("a budget stop is a result, not an error: %v", err)
	}
	pid, _ := f.calls(t)[0]["pid"].(float64)
	return res, log.String(), int(pid), took
}

func TestClaudeStopsTheRoundAtItsBudget(t *testing.T) {
	spend := []map[string]any{assistant("m1", tokens(1_000_000, 0), nil, map[string]any{"type": "text", "text": "big step"})} // $4 at opus-5-5
	cases := []struct {
		name   string
		limit  Limit
		events []map[string]any
	}{
		{"usd", Limit{USD: 3, Tokens: 1e9, Minutes: 100}, spend},
		{"tokens", Limit{USD: 100, Tokens: 500_000, Minutes: 100}, spend},
		{"minutes", Limit{USD: 100, Tokens: 1e9, Minutes: 0.02}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, log, pid, took := breach(t, c.limit, c.events)
			if took > 30*time.Second {
				t.Fatalf("the round ran %v: the process was not stopped", took)
			}
			if pid == 0 || pidAlive(pid) {
				t.Fatalf("fake claude pid %d still alive", pid)
			}
			if res.OverBudget != c.name || res.SessionID != "s-9" || res.StopReason != "over budget: "+c.name {
				t.Fatalf("res %+v", res)
			}
			if c.events != nil && (math.Abs(res.CostUSD-4) > 1e-9 || !res.CostKnown || res.Usage.Input != 1_000_000) {
				t.Fatalf("the partial spend is kept: %+v", res)
			}
			if !strings.Contains(log, "it reached the run's "+c.name+" budget") || strings.Contains(log, "never") {
				t.Fatalf("log %q", log)
			}
		})
	}
}

func TestClaudeUnderBudgetRunsToTheEnd(t *testing.T) {
	f := newFakeClaude(t, []map[string]any{{"session_id": "s-5", "cost": 1.5, "result": "fine",
		"events": []map[string]any{assistant("m1", tokens(100_000, 0), nil)}}})
	res, _ := run(t, Claude{Bin: f.bin}, RunRequest{Dir: f.dir, Prompt: "p", Env: os.Environ(), Limit: Limit{USD: 1, Tokens: 1e9, Minutes: 10}})
	// the stream priced the round at $0.40, under the $1 left; the result's own total decides afterwards
	if res.OverBudget != "" || res.Transcript != "fine" || res.CostUSD != 1.5 {
		t.Fatalf("res %+v", res)
	}
}
