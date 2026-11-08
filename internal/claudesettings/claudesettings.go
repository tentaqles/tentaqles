// Package claudesettings renders the tq-managed part of a Claude identity's
// settings.json: a few env vars, the client's language and the status line. Every identity gets the
// same baseline plus what its manifest adds, so identities stop drifting
// apart. tq writes no permission setting; its hook only denies.
//
// tq only touches what it owns. What it wrote last time is recorded in
// <dir>/.tq-settings-state.json; on the next render that is replaced by the
// new desired set, and everything the user added by hand is kept.
package claudesettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

const stateFile = ".tq-settings-state.json"

// Managed is the desired tq-owned content of settings.json.
type Managed struct {
	Env        map[string]string
	StatusLine map[string]any // nil: leave the status line alone
	// Language is the name Claude Code's language setting gets ("Portuguese (Brazil)"); "" leaves it.
	Language string
}

// Owned records what tq wrote, so the next render can take it back.
type Owned struct {
	EnvKeys    []string `json:"env_keys,omitempty"`
	StatusLine bool     `json:"status_line,omitempty"`
	Language   bool     `json:"language,omitempty"`
}

// baselineEnv tunes plugins that otherwise cost a lot: the security-guidance
// plugin's Stop review ran a headless Opus session after every file-changing
// turn (373 in 90 days). Commit/push reviews stay on, on Sonnet: they use the
// plugin's agentic path, which reads SG_AGENTIC_MODEL (default Opus), not
// SECURITY_REVIEW_MODEL (90 of 92 daily reviews ran on Opus in those 90 days).
var baselineEnv = map[string]string{
	"ENABLE_STOP_REVIEW":    "0",
	"SECURITY_REVIEW_MODEL": "claude-sonnet-5-5",
	"SG_AGENTIC_MODEL":      "claude-sonnet-5-5",
}

// Desired computes the managed settings for a workspace manifest. tqExe is
// the tq binary the status line should call ("" to leave it alone).
func Desired(m *manifest.Manifest, tqExe string) Managed {
	want := Managed{Env: map[string]string{}}
	for k, v := range baselineEnv {
		want.Env[k] = v
	}
	for k, v := range m.Claude.Env {
		want.Env[k] = v
	}
	want.Language = m.ClientLanguage().Name
	if tqExe != "" {
		want.StatusLine = map[string]any{
			"type":    "command",
			"command": fmt.Sprintf("%q statusline", filepath.ToSlash(tqExe)),
			"padding": 0,
		}
	}
	return want
}

// Apply merges want into current, taking back what prev says tq owned.
func Apply(current map[string]any, want Managed, prev Owned) (map[string]any, Owned) {
	next := deepCopy(current)
	var owned Owned

	env, _ := next["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	for _, k := range prev.EnvKeys {
		if _, still := want.Env[k]; !still {
			delete(env, k)
		}
	}
	for k, v := range want.Env {
		env[k] = v
		owned.EnvKeys = append(owned.EnvKeys, k)
	}
	sort.Strings(owned.EnvKeys)
	if len(env) == 0 {
		delete(next, "env")
	} else {
		next["env"] = env
	}

	// The language follows the manifest. A language the user set by hand, before tq ever wrote one,
	// stays theirs.
	if want.Language != "" {
		if _, has := next["language"]; !has || prev.Language {
			next["language"] = want.Language
			owned.Language = true
		}
	} else if prev.Language {
		delete(next, "language")
	}

	if want.StatusLine != nil {
		cur, _ := next["statusLine"].(map[string]any)
		if cur == nil || prev.StatusLine || replaceableStatusLine(cur) {
			next["statusLine"] = want.StatusLine
			owned.StatusLine = true
		}
	}
	return next, owned
}

// replaceableStatusLine reports status lines tq takes over: unpinned npx
// packages (they re-resolve @latest on every refresh) and tq's own.
func replaceableStatusLine(sl map[string]any) bool {
	cmd, _ := sl["command"].(string)
	isTq := (strings.Contains(cmd, "tq") || strings.Contains(cmd, "tentaqles")) && strings.HasSuffix(strings.TrimSpace(cmd), " statusline")
	return strings.Contains(cmd, "@latest") || strings.Contains(cmd, "ccstatusline") || isTq
}

// Result describes one render.
type Result struct {
	Path    string
	Changed bool
	Changes []string // human-readable summary
}

// Render brings <dir>/settings.json in line with want. With dryRun nothing
// is written.
func Render(dir string, want Managed, dryRun bool) (Result, error) {
	path := filepath.Join(dir, "settings.json")
	res := Result{Path: path}
	current, err := readJSONMap(path)
	if err != nil {
		return res, err
	}
	next, owned := Apply(current, want, loadOwned(dir))
	res.Changes = summarize(current, next)
	res.Changed = !reflect.DeepEqual(normalize(current), normalize(next))
	if dryRun || !res.Changed {
		return res, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, err
	}
	if err := writeJSONAtomic(path, next); err != nil {
		return res, err
	}
	return res, saveOwned(dir, owned)
}

func summarize(cur, next map[string]any) []string {
	var out []string
	if !reflect.DeepEqual(normalize(cur["env"]), normalize(next["env"])) {
		out = append(out, "env: updated")
	}
	if !reflect.DeepEqual(cur["language"], next["language"]) {
		out = append(out, fmt.Sprintf("language: %v", next["language"]))
	}
	if !reflect.DeepEqual(normalize(cur["statusLine"]), normalize(next["statusLine"])) {
		out = append(out, "statusLine: tentaqles statusline")
	}
	return out
}

// loadOwned reads the state file. One that is missing, unreadable or does
// not parse claims nothing.
func loadOwned(dir string) Owned {
	var o Owned
	raw, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil || json.Unmarshal(raw, &o) != nil {
		return Owned{}
	}
	return o
}

func saveOwned(dir string, o Owned) error {
	raw, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, stateFile), append(raw, '\n'), 0o600)
}

func deepCopy(m map[string]any) map[string]any {
	raw, _ := json.Marshal(m)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func normalize(v any) any {
	raw, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

// readJSONMap reads a JSON object file; a missing or empty file is an empty map.
func readJSONMap(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// WriteJSONAtomic writes m to path as indented JSON via a temp file + rename,
// so readers never observe a partially-written file.
func writeJSONAtomic(path string, m map[string]any) error {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}

	if err := os.Rename(tmp, path); err != nil {
		if errors.Is(err, os.ErrExist) || os.IsExist(err) {
			if rmErr := os.Remove(path); rmErr != nil {
				os.Remove(tmp)
				return rmErr
			}
			if err := os.Rename(tmp, path); err != nil {
				os.Remove(tmp)
				return err
			}
			return nil
		}
		os.Remove(tmp)
		return err
	}
	return nil
}
