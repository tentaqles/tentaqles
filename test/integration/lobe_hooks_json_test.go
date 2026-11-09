package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// lobeHook and lobeGroup are the parts of plugin/hooks/hooks.json this test reads.
type lobeHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type lobeGroup struct {
	Matcher string     `json:"matcher"`
	Hooks   []lobeHook `json:"hooks"`
}

type lobeHooksFile struct {
	Hooks map[string][]lobeGroup `json:"hooks"`
}

// camelWord splits a hook key at each capital: PostToolUseFailure -> post-tool-use-failure.
var camelWord = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func dashedEvent(key string) string {
	return strings.ToLower(camelWord.ReplaceAllString(key, "$1-$2"))
}

func loadPluginHooks(t *testing.T) map[string][]lobeGroup {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f lobeHooksFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v", err)
	}
	return f.Hooks
}

// The guard's events. Their groups hold tq_hook.sh only; every other group holds lobe_hook.sh only.
var guardEvents = map[string]bool{"SessionStart": true, "PreToolUse": true}

func TestLobeHookTimeoutsRegistered(t *testing.T) {
	hooks := loadPluginHooks(t)
	if len(hooks) == 0 {
		t.Fatal("hooks.json registers no events")
	}
	for event, groups := range hooks {
		for _, g := range groups {
			if len(g.Hooks) == 0 {
				t.Errorf("%s: a group has no hooks", event)
			}
			for _, h := range g.Hooks {
				isLobe := strings.Contains(h.Command, "lobe_hook.sh")
				isTq := strings.Contains(h.Command, "tq_hook.sh")
				switch {
				case isLobe == isTq:
					t.Errorf("%s: hook must run exactly one of lobe_hook.sh or tq_hook.sh: %q", event, h.Command)
				case isTq:
					if !guardEvents[event] {
						t.Errorf("%s: a guard hook (tq_hook.sh) sits outside the guard's events: %q", event, h.Command)
					}
					if h.Timeout != 15 {
						t.Errorf("%s: guard hook timeout = %d, want 15", event, h.Timeout)
					}
				case isLobe:
					if guardEvents[event] {
						t.Errorf("%s: a memory hook (lobe_hook.sh) sits in the guard's group: %q", event, h.Command)
					}
					if h.Timeout != 5 {
						t.Errorf("%s: memory hook timeout = %d, want 5", event, h.Timeout)
					}
					fields := strings.Fields(h.Command)
					want := dashedEvent(event)
					if len(fields) == 0 || fields[len(fields)-1] != want {
						t.Errorf("%s: command must end with the event word %q: %q", event, want, h.Command)
					}
				}
			}
		}
	}
}

func TestLobeHookScriptFailsOpen(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "scripts", "lobe_hook.sh"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if strings.Contains(src, "set -e") {
		t.Error("lobe_hook.sh must not use set -e: a failing step would turn into a non-zero exit")
	}
	if !strings.Contains(src, "exit 0") {
		t.Error("lobe_hook.sh must exit 0")
	}
	for i, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "tq_hook.sh") {
			t.Errorf("lobe_hook.sh line %d runs or names the guard script: %q", i+1, line)
		}
	}
}
