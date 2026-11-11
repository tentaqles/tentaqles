package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// lobeStatusHooks runs `tentaqles lobe status --json --client <name>` and returns the hooks object.
func lobeStatusHooks(t *testing.T, name string) map[string]string {
	t.Helper()
	out, err := runLobe(t, "", "lobe", "status", "--json", "--client", name)
	if err != nil {
		t.Fatalf("lobe status: %v\n%s", err, out)
	}
	var doc struct {
		Hooks map[string]string `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &doc); err != nil {
		t.Fatalf("lobe status --json is not JSON: %v\n%s", err, out)
	}
	return doc.Hooks
}

// TestLobeStatusShowsTheCodexHandler: after a settings render, the hooks object of a client with codex holds
// the lobe's Codex handler as trusted; once the lobe handler is edited in config.toml it holds modified.
func TestLobeStatusShowsTheCodexHandler(t *testing.T) {
	mkCodexWS(t, "acme", false)
	clearAgentMarkers(t)
	if out, err := runTq(t, "settings", "render", "acme"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := lobeStatusHooks(t, "acme")["codex"]; got != "trusted" {
		t.Fatalf(`after a render: hooks.codex = %q, want "trusted"`, got)
	}

	editCodex(t, "acme", func(root map[string]any) {
		hooks := root["hooks"].(map[string]any)
		for _, g := range asList(hooks["PostToolUse"]) {
			for _, h := range asList(g.(map[string]any)["hooks"]) {
				h.(map[string]any)["timeout"] = int64(6)
			}
		}
	})
	if got := lobeStatusHooks(t, "acme")["codex"]; got != "modified" {
		t.Fatalf(`after an edit of the lobe handler: hooks.codex = %q, want "modified"`, got)
	}
	out, err := runLobe(t, "", "lobe", "status", "--client", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "hooks: codex modified\n") {
		t.Fatalf("text status has no codex line:\n%s", out)
	}
}

// TestLobeStatusNoCodexKeyWithoutCodex: a client without codex in its scope has no codex entry in the hooks
// object, in the JSON form.
func TestLobeStatusNoCodexKeyWithoutCodex(t *testing.T) {
	base := newBase(t)
	healthyClient(t, base, "plain")
	out, err := runLobe(t, "", "lobe", "status", "--json", "--client", "plain")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, `"codex"`) {
		t.Fatalf("a client without codex has a codex key: %s", out)
	}
	if !strings.Contains(out, `"hooks":{"claude":"none"}`) {
		t.Fatalf("hooks object is not the claude entry alone: %s", out)
	}
}
