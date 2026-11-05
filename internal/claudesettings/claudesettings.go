// Package claudesettings renders the tq-managed part of a Claude identity's
// settings.json: a few env vars and the status line. Every identity gets the
// same baseline plus what its manifest adds, so identities stop drifting
// apart. tq writes no permission setting (PRD v2 §13: permission modes of
// any kind are a non-goal); its hook only denies.
//
// tq only touches what it owns. What it wrote last time is recorded in
// <dir>/.tq-settings-state.json; on the next render that is replaced by the
// new desired set, and everything the user added by hand is kept.
//
// Older renders also wrote permissions.defaultMode, permissions.allow/deny/ask
// and skipDangerousModePermissionPrompt. The first render with this code
// cleans them (cleanLegacyPermissions): the allow/deny/ask entries the state
// file records as tq's are removed, entries the user added stay, and an
// emptied permissions object goes; defaultMode and the skip flag, which the
// old state file did not record, go only when an old render is evidenced (a
// state file older than stateSchema) and the value is one tq wrote.
package claudesettings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/bundle"
	"github.com/tentaqles/tentaqles/internal/manifest"
)

const stateFile = ".tq-settings-state.json"

// stateSchema is the schema of the state file this code writes. A state file
// without it was written by a render that also wrote permission settings.
const stateSchema = 2

// Managed is the desired tq-owned content of settings.json.
type Managed struct {
	Env        map[string]string
	StatusLine map[string]any // nil: leave the status line alone
	// OldDefaultMode is the permissions.defaultMode an older render wrote
	// for this manifest (oldModeFor of its dropped claude.permission_mode):
	// the only value the cleaning removes. Never written.
	OldDefaultMode string
}

// Owned records what tq wrote, so the next render can take it back. Allow,
// Deny and Ask are only read: they are the permission entries an older
// render recorded, and a render with this code removes them and records
// none.
type Owned struct {
	Schema     int      `json:"schema,omitempty"`
	Allow      []string `json:"allow,omitempty"`
	Deny       []string `json:"deny,omitempty"`
	Ask        []string `json:"ask,omitempty"`
	EnvKeys    []string `json:"env_keys,omitempty"`
	StatusLine bool     `json:"status_line,omitempty"`
	// found is set by loadOwned when the state file exists.
	found bool
}

// legacyRender reports a state file written by a render that also wrote
// permission settings: the evidence cleanLegacyPermissions needs before it
// removes defaultMode or skipDangerousModePermissionPrompt.
func (o Owned) legacyRender() bool { return o.found && o.Schema < stateSchema }

// oldModeFor is what an older render wrote as permissions.defaultMode for
// a manifest's claude.permission_mode, on every render: "auto" for none or
// auto, "bypassPermissions" for bypass, any other value as it was.
func oldModeFor(m string) string {
	switch m {
	case "", "auto":
		return "auto"
	case "bypass":
		return "bypassPermissions"
	}
	return m
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
	want := Managed{Env: map[string]string{}, OldDefaultMode: oldModeFor(m.OldPermissionMode)}
	for k, v := range baselineEnv {
		want.Env[k] = v
	}
	for k, v := range m.Claude.Env {
		want.Env[k] = v
	}
	if tqExe != "" {
		want.StatusLine = map[string]any{
			"type":    "command",
			"command": fmt.Sprintf("%q statusline", filepath.ToSlash(tqExe)),
			"padding": 0,
		}
	}
	return want
}

// cleanLegacyPermissions removes from next the permission settings an older
// render wrote, and only those, decided from the evidence: the allow/deny/
// ask entries prev (a state file that parsed) records as tq's (an entry the
// user added stays); when prev is an older render's state file, the
// defaultMode equal to oldMode (what that render wrote for this manifest's
// old permission_mode; any other value is the user's) and a
// skipDangerousModePermissionPrompt of true (an older render wrote it on
// every render). A permissions object this emptied is removed; one that
// was already empty, or that still holds anything else, is left.
func cleanLegacyPermissions(next map[string]any, prev Owned, oldMode string) {
	perms, ok := next["permissions"].(map[string]any)
	removed := false
	if ok {
		for key, owned := range map[string][]string{"allow": prev.Allow, "deny": prev.Deny, "ask": prev.Ask} {
			cur, present := perms[key]
			if !present || len(owned) == 0 {
				continue
			}
			list := toStrings(cur)
			kept := minus(list, owned)
			if len(kept) == len(list) {
				continue
			}
			removed = true
			if len(kept) == 0 {
				delete(perms, key)
			} else {
				perms[key] = toAny(kept)
			}
		}
		if mode, isStr := perms["defaultMode"].(string); isStr && prev.legacyRender() && oldMode != "" && mode == oldMode {
			delete(perms, "defaultMode")
			removed = true
		}
		if removed && len(perms) == 0 {
			delete(next, "permissions")
		}
	}
	if skip, isBool := next["skipDangerousModePermissionPrompt"].(bool); isBool && skip && prev.legacyRender() {
		delete(next, "skipDangerousModePermissionPrompt")
	}
}

// Apply merges want into current, taking back what prev says tq owned.
func Apply(current map[string]any, want Managed, prev Owned) (map[string]any, Owned) {
	next := deepCopy(current)
	owned := Owned{Schema: stateSchema}
	cleanLegacyPermissions(next, prev, want.OldDefaultMode)

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
// packages (they re-resolve @latest on every refresh) and older tq ones.
func replaceableStatusLine(sl map[string]any) bool {
	cmd, _ := sl["command"].(string)
	isTq := strings.Contains(cmd, "tq") && strings.HasSuffix(strings.TrimSpace(cmd), " statusline")
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
	current, err := bundle.ReadJSONMap(path)
	if err != nil {
		return res, err
	}
	prev := loadOwned(dir)
	next, owned := Apply(current, want, prev)
	res.Changes = summarize(current, next)
	res.Changed = !reflect.DeepEqual(normalize(current), normalize(next))
	// An old state file is rewritten even when settings.json needs no
	// change, so it stops being evidence of an older render: a defaultMode
	// the user sets by hand afterwards is never taken for tq's.
	if dryRun || (!res.Changed && !prev.legacyRender()) {
		return res, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, err
	}
	if res.Changed {
		if err := bundle.WriteJSONAtomic(path, next); err != nil {
			return res, err
		}
	}
	return res, saveOwned(dir, owned)
}

func summarize(cur, next map[string]any) []string {
	var out []string
	cp, _ := cur["permissions"].(map[string]any)
	np, _ := next["permissions"].(map[string]any)
	if cp == nil {
		cp = map[string]any{}
	}
	if np == nil {
		np = map[string]any{}
	}
	if v, had := cp["defaultMode"]; had {
		if _, has := np["defaultMode"]; !has {
			out = append(out, fmt.Sprintf("permissions.defaultMode: removed (was %v)", v))
		}
	}
	for _, k := range []string{"allow", "deny", "ask"} {
		if rm := minus(toStrings(cp[k]), toStrings(np[k])); len(rm) > 0 {
			out = append(out, fmt.Sprintf("permissions.%s: removed %d tq entries (%s)", k, len(rm), strings.Join(rm, ", ")))
		}
	}
	if _, had := cur["permissions"]; had {
		if _, has := next["permissions"]; !has {
			out = append(out, "permissions: removed (empty)")
		}
	}
	_, had := cur["skipDangerousModePermissionPrompt"]
	_, has := next["skipDangerousModePermissionPrompt"]
	if had && !has {
		out = append(out, "skipDangerousModePermissionPrompt: removed")
	}
	if !reflect.DeepEqual(normalize(cur["env"]), normalize(next["env"])) {
		out = append(out, "env: updated")
	}
	if !reflect.DeepEqual(normalize(cur["statusLine"]), normalize(next["statusLine"])) {
		out = append(out, "statusLine: tq statusline")
	}
	return out
}

// loadOwned reads the state file. One that is missing, unreadable or does
// not parse claims nothing: no entry is tq's and it is no evidence of an
// older render.
func loadOwned(dir string) Owned {
	var o Owned
	raw, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil || json.Unmarshal(raw, &o) != nil {
		return Owned{}
	}
	o.found = true
	return o
}

func saveOwned(dir string, o Owned) error {
	raw, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, stateFile), append(raw, '\n'), 0o600)
}

func toStrings(v any) []string {
	var out []string
	switch xs := v.(type) {
	case []any:
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, xs...)
	}
	return out
}

func toAny(xs []string) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

func minus(a, b []string) []string {
	drop := map[string]bool{}
	for _, x := range b {
		drop[x] = true
	}
	var out []string
	for _, x := range a {
		if !drop[x] {
			out = append(out, x)
		}
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
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
