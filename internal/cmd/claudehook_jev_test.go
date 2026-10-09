package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// fakeJev is a TypeSafe stand-in on 127.0.0.1 (an allowed base_url).
func fakeJev(t *testing.T, answer func(questions map[string]any) map[string]any, status int) (string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Questions map[string]any `json:"questions"`
		}
		_ = json.Unmarshal(raw, &body)
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answer(body.Questions)})
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &calls
}

func noulAll(p float64) func(map[string]any) map[string]any {
	return func(qs map[string]any) map[string]any {
		out := map[string]any{}
		for id := range qs {
			out[id] = map[string]any{"noul": p}
		}
		return out
	}
}

func jevWorkspace(t *testing.T, url, mode string) string {
	t.Helper()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	return setupTrustedWorkspaceWithManifest(t, "acme", "decision:\n  backend: typesafe\n  mode: "+mode+"\n  base_url: "+url+"/v1/systemone\n")
}

const dropEdit = `{"file_path":"db/m/0042.sql","old_string":"","new_string":"DROP TABLE customer_orders;"}`

func TestJev_EnforceAsksOnRiskyEdit(t *testing.T) {
	url, calls := fakeJev(t, noulAll(0.95), 0)
	ws := jevWorkspace(t, url, "enforce")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Edit", dropEdit))
	if code != 0 || !strings.Contains(askReason(t, out), "jev/destructive-sql") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestJev_ShadowLogsOnly(t *testing.T) {
	url, calls := fakeJev(t, noulAll(0.95), 0)
	ws := jevWorkspace(t, url, "shadow")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Edit", dropEdit))
	if code != 0 || out != "" || calls.Load() != 1 {
		t.Fatalf("shadow acted: code=%d out=%q calls=%d", code, out, calls.Load())
	}
	raw, err := os.ReadFile(filepath.Join(paths.Home(), "decide", "judgments.jsonl"))
	if err != nil || !strings.Contains(string(raw), `"jev/destructive-sql":"deny"`) || strings.Contains(string(raw), "customer_orders") {
		t.Fatalf("judgment log: %v %s", err, raw)
	}
}

func TestJev_UnrelatedCallsNeverReachJev(t *testing.T) {
	url, calls := fakeJev(t, noulAll(0.95), 0)
	ws := jevWorkspace(t, url, "enforce")
	runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Edit", `{"file_path":"README.md","old_string":"a","new_string":"b"}`))
	runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", `{"command":"git stash drop"}`))
	if calls.Load() != 0 {
		t.Fatalf("pre-filter let %d unrelated calls through", calls.Load())
	}
}

func TestJev_OutageFailsOpenToRulesAndTripsBreaker(t *testing.T) {
	url, calls := fakeJev(t, nil, 503)
	ws := jevWorkspace(t, url, "enforce")
	for i := 0; i < 3; i++ {
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Edit", dropEdit))
		if code != 0 || out != "" {
			t.Fatalf("outage changed the decision: code=%d out=%q", code, out)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("breaker should stop retries after one failure; calls = %d", calls.Load())
	}
}

func agentPayload(t *testing.T, cwd, transcript, input string) string {
	return `{"tool_name":"Agent","cwd":` + jsonPath(t, cwd) + `,"transcript_path":` + jsonPath(t, transcript) + `,"tool_input":` + input + `}`
}

func opusTranscript(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(`{"type":"assistant","message":{"model":"claude-opus-5-5"}}`+"\n"), 0o600)
	return p
}

func TestJev_RouteSetsCheaperModel(t *testing.T) {
	url, _ := fakeJev(t, func(map[string]any) map[string]any {
		return map[string]any{"tier": map[string]any{"choice": "haiku", "confidence": 0.93}}
	}, 0)
	ws := jevWorkspace(t, url, "enforce")
	in := `{"prompt":"List every file that imports the logger","description":"find logger imports","subagent_type":"general-purpose"}`
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, agentPayload(t, ws, opusTranscript(t), in))
	var v struct {
		H struct {
			Decision string         `json:"permissionDecision"`
			Input    map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	// Routing rewrites the input but never grants permission.
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v.H.Decision != "" || v.H.Input == nil {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if v.H.Input["model"] != "haiku" || v.H.Input["prompt"] != "List every file that imports the logger" || v.H.Input["subagent_type"] != "general-purpose" {
		t.Fatalf("updatedInput must keep every field and add the model: %v", v.H.Input)
	}
}

func TestJev_RouteLeavesExplicitModelAlone(t *testing.T) {
	url, calls := fakeJev(t, func(map[string]any) map[string]any {
		return map[string]any{"tier": map[string]any{"choice": "haiku", "confidence": 0.99}}
	}, 0)
	ws := jevWorkspace(t, url, "enforce")
	in := `{"prompt":"design the auth flow","model":"opus"}`
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, agentPayload(t, ws, opusTranscript(t), in))
	if code != 0 || out != "" || calls.Load() != 0 {
		t.Fatalf("explicit model touched: code=%d out=%q calls=%d", code, out, calls.Load())
	}
}

func TestJev_AgentStillGoesThroughPolicy(t *testing.T) {
	url, calls := fakeJev(t, func(map[string]any) map[string]any {
		return map[string]any{"tier": map[string]any{"choice": "haiku", "confidence": 0.99}}
	}, 0)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "decision:\n  backend: typesafe\n  mode: enforce\n  base_url: "+url+"/v1/systemone\n"+
		"guard:\n  rules:\n    - id: acme/no-prod-agents\n      action: deny\n      tool: Agent\n      content: 'prod-db'\n      reason: no subagents on the prod database\n")
	in := `{"prompt":"run the cleanup against prod-db","subagent_type":"general-purpose"}`
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, agentPayload(t, ws, opusTranscript(t), in))
	if code != 2 || !strings.Contains(errOut, "acme/no-prod-agents") || out != "" || calls.Load() != 0 {
		t.Fatalf("policy skipped for Agent: code=%d out=%q err=%q calls=%d", code, out, errOut, calls.Load())
	}
}
