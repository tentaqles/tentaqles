package codexcfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HooksFile is Codex's user-layer hooks file, relative to CODEX_HOME.
const HooksFile = "hooks.json"

// The commands tq registers. Bare `tq` (resolved on PATH) keeps the entry,
// and so its Codex trust hash, stable across tq upgrades and install paths.
const (
	PreToolUseCommand   = "tq claude-hook pre-tool-use --agent codex"
	SessionStartCommand = "tq claude-hook session-start --agent codex"
)

// hookEvents are the events tq registers, in file order.
var hookEvents = []struct {
	Event, Command, Status string
}{
	{"PreToolUse", PreToolUseCommand, "tq guard"},
	{"SessionStart", SessionStartCommand, "tq context"},
}

// isOurs reports whether a handler command is one tq wrote (any path to tq,
// any event).
func isOurs(cmd string) bool {
	return strings.Contains(cmd, "claude-hook") && strings.Contains(cmd, "--agent codex")
}

func handlerCommand(h any) string {
	m, ok := h.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m["command"].(string)
	return s
}

// groupOwnership: 0 none of tq's handlers, 1 only tq's, 2 mixed.
func groupOwnership(g any) int {
	m, ok := g.(map[string]any)
	if !ok {
		return 0
	}
	hs, _ := m["hooks"].([]any)
	ours, theirs := 0, 0
	for _, h := range hs {
		if isOurs(handlerCommand(h)) {
			ours++
		} else {
			theirs++
		}
	}
	switch {
	case ours == 0:
		return 0
	case theirs == 0:
		return 1
	}
	return 2
}

func ourGroup(command, status string) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks": []any{map[string]any{
			"type":          "command",
			"command":       command,
			"timeout":       json.Number("30"),
			"statusMessage": status,
		}},
	}
}

func decodeHooks(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

// planHooks computes the new hooks.json content. remove: the file then holds
// nothing and is deleted. conflict: nothing is safe to write.
func planHooks(raw []byte, exists, want bool) (next []byte, remove bool, conflict string) {
	doc := map[string]any{}
	if exists {
		d, err := decodeHooks(raw)
		if err != nil {
			return nil, false, "hooks.json is not valid JSON; fix it to let tq add its hook"
		}
		doc = d
	}
	hooks := map[string]any{}
	if v, ok := doc["hooks"]; ok {
		h, ok := v.(map[string]any)
		if !ok {
			return nil, false, "hooks.json: \"hooks\" is not an object"
		}
		hooks = h
	}
	for _, ev := range hookEvents {
		var groups []any
		if v, ok := hooks[ev.Event]; ok {
			g, ok := v.([]any)
			if !ok {
				return nil, false, fmt.Sprintf("hooks.json: hooks.%s is not a list", ev.Event)
			}
			groups = g
		}
		at := -1
		var kept []any
		for _, g := range groups {
			switch groupOwnership(g) {
			case 2:
				return nil, false, fmt.Sprintf("hooks.json: a hooks.%s group mixes tq's handler with others; split it to let tq manage it", ev.Event)
			case 1:
				if at < 0 {
					at = len(kept)
				}
				continue
			}
			kept = append(kept, g)
		}
		if want {
			mine := ourGroup(ev.Command, ev.Status)
			if at < 0 {
				kept = append(kept, mine)
			} else {
				// Keep tq's position, so the user's entries (and their
				// trust keys, which carry the index) do not move.
				kept = append(kept[:at], append([]any{mine}, kept[at:]...)...)
			}
		}
		if len(kept) == 0 {
			delete(hooks, ev.Event)
		} else {
			hooks[ev.Event] = kept
		}
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	} else {
		doc["hooks"] = hooks
	}
	if len(doc) == 0 {
		return nil, true, ""
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, false, "hooks.json: " + err.Error()
	}
	return append(b, '\n'), false, ""
}

// sameJSON compares two hooks.json contents by meaning, so a file that
// already holds tq's entries is not rewritten for formatting alone.
func sameJSON(a, b []byte) bool {
	da, err1 := decodeHooks(a)
	db, err2 := decodeHooks(b)
	if err1 != nil || err2 != nil {
		return false
	}
	ja, _ := json.Marshal(da)
	jb, _ := json.Marshal(db)
	return bytes.Equal(ja, jb)
}

// FindHook returns the position of tq's handler for event in
// dir/hooks.json (group index, handler index), as Codex keys its trust
// records.
func FindHook(dir, event string) (group, handler int, ok bool) {
	b, err := os.ReadFile(filepath.Join(dir, HooksFile))
	if err != nil {
		return 0, 0, false
	}
	return HookIndex(b, event)
}

// HookIndex is FindHook over hooks.json content.
func HookIndex(b []byte, event string) (group, handler int, ok bool) {
	doc, err := decodeHooks(b)
	if err != nil {
		return 0, 0, false
	}
	hooks, _ := doc["hooks"].(map[string]any)
	groups, _ := hooks[event].([]any)
	for gi, g := range groups {
		m, _ := g.(map[string]any)
		hs, _ := m["hooks"].([]any)
		for hi, h := range hs {
			if isOurs(handlerCommand(h)) {
				return gi, hi, true
			}
		}
	}
	return 0, 0, false
}
