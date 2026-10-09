package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// tline builds one transcript JSONL record.
func tline(t *testing.T, typ string, content any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": typ, "message": map[string]any{"role": typ, "content": content}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func toolUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func toolResult(id string, isErr bool) map[string]any {
	return map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": isErr, "content": "out"}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// claimedTranscript edits a file and claims tests pass; withRun adds a
// successful test run after the edit.
func claimedTranscript(t *testing.T, withRun bool) string {
	lines := []string{
		tline(t, "user", "fix the parser bug"),
		tline(t, "assistant", []any{toolUse("e1", "Edit", map[string]any{"file_path": "parser.go", "old_string": "a", "new_string": "b"})}),
		tline(t, "user", []any{toolResult("e1", false)}),
	}
	if withRun {
		lines = append(lines,
			tline(t, "assistant", []any{toolUse("b1", "Bash", map[string]any{"command": "go test ./..."})}),
			tline(t, "user", []any{toolResult("b1", false)}))
	}
	lines = append(lines, tline(t, "assistant", []any{text("Fixed. All tests pass.")}))
	return writeTranscript(t, lines...)
}

func stopPayload(t *testing.T, cwd, transcript, session string, active bool) string {
	raw, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "cwd": cwd, "transcript_path": transcript,
		"session_id": session, "stop_hook_active": active})
	return string(raw)
}

func judgments(t *testing.T) string {
	raw, _ := os.ReadFile(filepath.Join(paths.Home(), "decide", "judgments.jsonl"))
	return string(raw)
}

func TestStop(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		claims    float64
		evidence  float64
		withRun   bool
		active    bool
		noEdits   bool
		wantBlock bool
		wantCalls int32
		wantLog   string
	}{
		{name: "enforce blocks a claim without evidence", mode: "enforce", claims: 0.95, wantBlock: true, wantCalls: 1, wantLog: `"applied":{"stop":"block"}`},
		{name: "shadow only logs", mode: "shadow", claims: 0.95, wantCalls: 1, wantLog: `"would":{"stop":"block"}`},
		{name: "evidence present", mode: "enforce", claims: 0.95, evidence: 0.9, withRun: true, wantCalls: 1, wantLog: `"kind":"stop"`},
		{name: "no claim", mode: "enforce", claims: 0.1, wantCalls: 1, wantLog: `"kind":"stop"`},
		{name: "stop_hook_active", mode: "enforce", claims: 0.95, active: true},
		{name: "no edits", mode: "enforce", claims: 0.95, noEdits: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var asked map[string]any
			url, calls := fakeJev(t, func(qs map[string]any) map[string]any {
				asked = qs
				out := map[string]any{"claims": map[string]any{"noul": tc.claims}}
				if _, ok := qs["evidence"]; ok {
					out["evidence"] = map[string]any{"noul": tc.evidence}
				}
				return out
			}, 0)
			ws := jevWorkspace(t, url, tc.mode)
			tr := claimedTranscript(t, tc.withRun)
			if tc.noEdits {
				tr = writeTranscript(t,
					tline(t, "user", "fix it"),
					tline(t, "assistant", []any{toolUse("e1", "Edit", map[string]any{"file_path": "a.go"})}),
					tline(t, "user", "what does it do now?"),
					tline(t, "assistant", []any{text("It works and all tests pass.")}))
			}
			code, out, _ := runHook(t, []string{"claude-hook", "stop"}, stopPayload(t, ws, tr, "sess-1", tc.active))
			if code != 0 {
				t.Fatalf("code = %d", code)
			}
			if calls.Load() != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", calls.Load(), tc.wantCalls)
			}
			if tc.wantBlock {
				var v struct{ Decision, Reason string }
				if json.Unmarshal([]byte(out), &v) != nil || v.Decision != "block" || !strings.Contains(v.Reason, "unverified") {
					t.Fatalf("want block, got %q", out)
				}
			} else if out != "" {
				t.Fatalf("want no output, got %q", out)
			}
			if tc.wantLog != "" && !strings.Contains(judgments(t), tc.wantLog) {
				t.Fatalf("judgment log missing %s: %s", tc.wantLog, judgments(t))
			}
			if tc.wantCalls > 0 {
				if _, ok := asked["evidence"]; ok != tc.withRun {
					t.Fatalf("evidence asked = %v, want %v", ok, tc.withRun)
				}
			}
			if strings.Contains(judgments(t), "parser") {
				t.Fatal("judgment log leaked transcript content")
			}
		})
	}
}

func TestStop_OncePerSession(t *testing.T) {
	url, calls := fakeJev(t, noulAll(0.95), 0)
	ws := jevWorkspace(t, url, "enforce")
	tr := claimedTranscript(t, false)
	_, out, _ := runHook(t, []string{"claude-hook", "stop"}, stopPayload(t, ws, tr, "sess-once", false))
	if !strings.Contains(out, `"block"`) {
		t.Fatalf("first stop should block: %q", out)
	}
	_, out, _ = runHook(t, []string{"claude-hook", "stop"}, stopPayload(t, ws, tr, "sess-once", false))
	if out != "" || calls.Load() != 1 {
		t.Fatalf("second stop in the same session acted: out=%q calls=%d", out, calls.Load())
	}
	if _, err := os.Stat(filepath.Join(paths.Home(), "decide", "stop", "sess-once")); err != nil {
		t.Fatalf("marker: %v", err)
	}
	// A different session is checked again.
	_, out, _ = runHook(t, []string{"claude-hook", "stop"}, stopPayload(t, ws, tr, "sess-two", false))
	if !strings.Contains(out, `"block"`) {
		t.Fatalf("new session should be checked: %q", out)
	}
}

func TestStop_UnsafeSessionIDStaysInDir(t *testing.T) {
	isolateHome(t)
	m := stopMarker(`..\..\evil`, "t.jsonl")
	if filepath.Dir(m) != filepath.Join(paths.Home(), "decide", "stop") {
		t.Fatalf("marker escaped: %s", m)
	}
}

func TestStop_OutageDoesNothingAndTripsBreaker(t *testing.T) {
	url, calls := fakeJev(t, nil, 503)
	ws := jevWorkspace(t, url, "enforce")
	tr := claimedTranscript(t, false)
	for i := 0; i < 3; i++ {
		code, out, _ := runHook(t, []string{"claude-hook", "stop"}, stopPayload(t, ws, tr, "sess-x", false))
		if code != 0 || out != "" {
			t.Fatalf("outage acted: code=%d out=%q", code, out)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("breaker should stop retries; calls = %d", calls.Load())
	}
}

func TestStop_BackendOffNeverCallsJev(t *testing.T) {
	url, calls := fakeJev(t, noulAll(0.95), 0)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "decision:\n  backend: off\n  mode: enforce\n  base_url: "+url+"/v1/systemone\n")
	_, out, _ := runHook(t, []string{"claude-hook", "stop"}, stopPayload(t, ws, claimedTranscript(t, false), "s", false))
	if out != "" || calls.Load() != 0 {
		t.Fatalf("backend off acted: out=%q calls=%d", out, calls.Load())
	}
}

func TestStop_GarbagePayload(t *testing.T) {
	isolateHome(t)
	code, out, _ := runHook(t, []string{"claude-hook", "stop"}, "not json")
	if code != 0 || out != "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// --- skill picker ---

func writeSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// skillEnv sets up user, plugin and project skills under a fake config dir.
func skillEnv(t *testing.T, ws string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	writeSkill(t, filepath.Join(cfg, "skills"), "pdf", "Read, merge and split PDF files.")
	inst := filepath.Join(t.TempDir(), "superpowers")
	writeSkill(t, filepath.Join(inst, "skills"), "brainstorming", "Explore intent before building a feature.")
	raw, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{
		"superpowers@market": []any{map[string]any{"scope": "user", "installPath": inst}}}})
	if err := os.MkdirAll(filepath.Join(cfg, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "plugins", "installed_plugins.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(ws, ".claude", "skills"), "deploy", "Deploy this service to staging.")
}

func promptPayload(t *testing.T, cwd, prompt string) string {
	raw, _ := json.Marshal(map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": cwd, "prompt": prompt, "session_id": "s"})
	return string(raw)
}

func TestPromptSubmit(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		pick       string
		confidence float64
		prompt     string
		wantHint   string
		wantCalls  int32
	}{
		{name: "enforce hints a confident pick", mode: "enforce", pick: "pdf", confidence: 0.9, prompt: "merge these two PDFs", wantHint: "Skill that may fit: pdf", wantCalls: 1},
		{name: "enforce plugin skill", mode: "enforce", pick: "superpowers:brainstorming", confidence: 0.85, prompt: "let's design a feature", wantHint: "Skill that may fit: superpowers:brainstorming", wantCalls: 1},
		{name: "shadow prints nothing", mode: "shadow", pick: "pdf", confidence: 0.99, prompt: "merge these two PDFs", wantCalls: 1},
		{name: "low confidence", mode: "enforce", pick: "pdf", confidence: 0.6, prompt: "merge these two PDFs", wantCalls: 1},
		{name: "none", mode: "enforce", pick: "none", confidence: 0.99, prompt: "what time is it", wantCalls: 1},
		{name: "unknown pick", mode: "enforce", pick: "rm-rf", confidence: 0.99, prompt: "do it", wantCalls: 1},
		{name: "slash command skipped", mode: "enforce", pick: "pdf", confidence: 0.99, prompt: "/pdf merge"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var options map[string]any
			url, calls := fakeJev(t, func(qs map[string]any) map[string]any {
				q, _ := qs["skill"].(map[string]any)
				options, _ = q["criteria"].(map[string]any)
				return map[string]any{"skill": map[string]any{"choice": tc.pick, "confidence": tc.confidence}}
			}, 0)
			ws := jevWorkspace(t, url, tc.mode)
			skillEnv(t, ws)
			code, out, _ := runHook(t, []string{"claude-hook", "prompt-submit"}, promptPayload(t, ws, tc.prompt))
			if code != 0 || calls.Load() != tc.wantCalls {
				t.Fatalf("code=%d calls=%d", code, calls.Load())
			}
			if tc.wantHint == "" {
				if out != "" {
					t.Fatalf("want no output, got %q", out)
				}
			} else {
				var v struct {
					H struct {
						Event   string `json:"hookEventName"`
						Context string `json:"additionalContext"`
						Dec     string `json:"decision"`
					} `json:"hookSpecificOutput"`
				}
				if json.Unmarshal([]byte(out), &v) != nil || v.H.Event != "UserPromptSubmit" || v.H.Context != tc.wantHint {
					t.Fatalf("hint: %q", out)
				}
				if strings.Contains(out, "block") {
					t.Fatalf("skill picker must never block: %q", out)
				}
			}
			if tc.wantCalls > 0 {
				for _, o := range []string{"pdf", "superpowers:brainstorming", "deploy", "none"} {
					if _, ok := options[o]; !ok {
						t.Fatalf("option %q missing from %v", o, options)
					}
				}
				if !strings.Contains(judgments(t), `"kind":"skill"`) {
					t.Fatalf("no skill judgment: %s", judgments(t))
				}
			}
		})
	}
}

func TestPromptSubmit_RedactsAndTruncatesPromptInLog(t *testing.T) {
	url, _ := fakeJev(t, func(map[string]any) map[string]any {
		return map[string]any{"skill": map[string]any{"choice": "none", "confidence": 0.9}}
	}, 0)
	ws := jevWorkspace(t, url, "shadow")
	skillEnv(t, ws)
	secret := "ghp_" + strings.Repeat("A1b2", 9)
	runHook(t, []string{"claude-hook", "prompt-submit"}, promptPayload(t, ws, "use token "+secret+" "+strings.Repeat("x", 200)))
	log := judgments(t)
	if strings.Contains(log, secret) || strings.Contains(log, strings.Repeat("x", 100)) || !strings.Contains(log, `"prompt":"use token`) {
		t.Fatalf("prompt not redacted/truncated: %s", log)
	}
}

func TestPromptSubmit_OutageTripsBreaker(t *testing.T) {
	url, calls := fakeJev(t, nil, 500)
	ws := jevWorkspace(t, url, "enforce")
	skillEnv(t, ws)
	for i := 0; i < 3; i++ {
		code, out, _ := runHook(t, []string{"claude-hook", "prompt-submit"}, promptPayload(t, ws, "merge PDFs"))
		if code != 0 || out != "" {
			t.Fatalf("outage acted: code=%d out=%q", code, out)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestPromptSubmit_CachesSkillIndex(t *testing.T) {
	url, _ := fakeJev(t, func(map[string]any) map[string]any {
		return map[string]any{"skill": map[string]any{"choice": "none", "confidence": 0.9}}
	}, 0)
	ws := jevWorkspace(t, url, "shadow")
	skillEnv(t, ws)
	runHook(t, []string{"claude-hook", "prompt-submit"}, promptPayload(t, ws, "hello there"))
	matches, _ := filepath.Glob(filepath.Join(paths.Home(), "decide", "skills-*.json"))
	if len(matches) != 1 {
		t.Fatalf("skill index cache files: %v", matches)
	}
}
