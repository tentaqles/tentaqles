package statusline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parse(t *testing.T, s string) Input {
	t.Helper()
	var in Input
	if err := json.Unmarshal([]byte(s), &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestRenderFromContextWindow(t *testing.T) {
	in := parse(t, `{"model":{"display_name":"Opus 5.5"},"cost":{"total_cost_usd":4.2},
	  "context_window":{"context_window_size":1000000,"current_usage":{"input_tokens":2000,"cache_read_input_tokens":300000,"cache_creation_input_tokens":10000}}}`)
	got := Render(in, Facts{Client: "tentaqles", GitEmail: "reach@tentaqles.ai"}, false)
	want := "tentaqles · reach@tentaqles.ai · Opus 5.5 · ctx 312k (31%) · $4.20"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if colored := Render(in, Facts{Client: "x"}, true); !strings.Contains(colored, yellow+"ctx 312k") {
		t.Errorf("312k should be in the warn band: %q", colored)
	}
}

func TestRenderHotBandAndNeutral(t *testing.T) {
	in := parse(t, `{"model":{"id":"claude-sonnet-5-5"},"context_window":{"current_usage":{"input_tokens":450000}}}`)
	got := Render(in, Facts{}, false)
	if got != "no workspace · claude-sonnet-5-5 · ctx 450k — compact or wrap up" {
		t.Fatalf("got %q", got)
	}
}

func TestContextTokensFallsBackToTranscript(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	lines := []string{
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"assistant","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":100}}}`,
		`{"type":"assistant","message":{"usage":{"input_tokens":20,"cache_read_input_tokens":200000,"cache_creation_input_tokens":5}}}`,
		`{"type":"user","message":{"content":"Configuração 🚀"}}`,
	}
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	if n := ContextTokens(Input{TranscriptPath: p}); n != 200025 {
		t.Fatalf("got %d", n)
	}
	if n := ContextTokens(Input{TranscriptPath: filepath.Join(t.TempDir(), "missing")}); n != 0 {
		t.Fatalf("missing transcript: %d", n)
	}
}
