// Package claudesettings renders the tq-managed part of a Claude identity's
// settings.json: permission mode, allow/deny/ask rules, a few env vars and
// the status line. Every identity gets the same baseline plus what its
// manifest adds, so identities stop drifting apart.
//
// tq only touches what it owns. The entries it wrote last time are recorded
// in <dir>/.tq-settings-state.json; on the next render those are replaced by
// the new desired set, and every entry the user added by hand is kept.
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

// Managed is the desired tq-owned content of settings.json.
type Managed struct {
	DefaultMode string
	Allow       []string
	Deny        []string
	Ask         []string
	Env         map[string]string
	StatusLine  map[string]any // nil: leave the status line alone
}

// Owned records what tq wrote, so the next render can take it back.
type Owned struct {
	Allow      []string `json:"allow,omitempty"`
	Deny       []string `json:"deny,omitempty"`
	Ask        []string `json:"ask,omitempty"`
	EnvKeys    []string `json:"env_keys,omitempty"`
	StatusLine bool     `json:"status_line,omitempty"`
}

// baselineAllow are commands that only read. They skip the auto-mode
// classifier, so nothing here may write files, run repo-defined code, or
// take an argument that turns it into one that does: no find (-exec,
// -delete), no sed (w/e commands), no test or build runners (package
// scripts can do anything), no npx (fetches and runs packages). Those stay
// with the classifier. tq's PreToolUse guard still runs first, so e.g.
// `cat .env` keeps asking.
var baselineAllow = []string{
	"Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)",
	"Bash(git rev-parse:*)", "Bash(git ls-files:*)", "Bash(git remote -v)",
	"Bash(git worktree list:*)",
	"Bash(gh pr view:*)", "Bash(gh pr list:*)", "Bash(gh pr checks:*)", "Bash(gh pr diff:*)",
	"Bash(gh run view:*)", "Bash(gh run list:*)", "Bash(gh issue view:*)", "Bash(gh issue list:*)",
	"Bash(ls:*)", "Bash(cat:*)", "Bash(head:*)", "Bash(tail:*)", "Bash(wc:*)", "Bash(grep:*)",
	"Bash(rg:*)", "Bash(which:*)", "Bash(pwd)",
	"Bash(tq doctor:*)", "Bash(tq list:*)", "Bash(tq version)", "Bash(tq secrets audit:*)", "Bash(tq dotenv keys:*)",
	"Bash(az account show:*)", "Bash(aws sts get-caller-identity:*)",
	"PowerShell(Get-ChildItem:*)", "PowerShell(Get-Content:*)", "PowerShell(Test-Path:*)",
	"PowerShell(Select-String:*)", "PowerShell(Get-Location)",
}

// baselineDeny mirrors the hardest tq guard rules as a second layer that
// still holds if the hook is ever missing.
var baselineDeny = []string{
	"Read(~/.ssh/id_*)", "Read(~/.aws/credentials)", "Read(~/.git-credentials)", "Read(~/.netrc)",
	"Read(~/.tentaqles/bundles/catalog.yaml)", "Read(~/.config/gh/hosts.yml)",
	"Bash(printenv)", "Bash(gh repo delete:*)",
}

// baselineEnv tunes plugins that otherwise cost a lot: the security-guidance
// plugin's Stop review ran a headless Opus session after every file-changing
// turn (373 in 90 days). Commit/push reviews stay on.
var baselineEnv = map[string]string{
	"ENABLE_STOP_REVIEW":    "0",
	"SECURITY_REVIEW_MODEL": "claude-sonnet-5-5",
}

// ModeFor maps a manifest permission_mode to the settings.json value.
func ModeFor(m string) string {
	switch m {
	case "", "auto":
		return "auto"
	case "bypass":
		return "bypassPermissions"
	}
	return m
}

// Desired computes the managed settings for a workspace manifest. tqExe is
// the tq binary the status line should call ("" to leave it alone).
// bypassAllowed mirrors `tq allow --bypass`: without it a manifest asking for
// bypass gets acceptEdits, exactly as `tq run` does.
func Desired(m *manifest.Manifest, bypassAllowed bool, tqExe string) Managed {
	mode := ModeFor(m.Claude.PermissionMode)
	if mode == "bypassPermissions" && !bypassAllowed {
		mode = "acceptEdits"
	}
	want := Managed{
		DefaultMode: mode,
		Allow:       append([]string{}, baselineAllow...),
		Deny:        append([]string{}, baselineDeny...),
		Env:         map[string]string{},
	}
	for k, v := range baselineEnv {
		want.Env[k] = v
	}
	if p := m.Claude.Permissions; p != nil {
		want.Allow = append(want.Allow, p.Allow...)
		want.Deny = append(want.Deny, p.Deny...)
		want.Ask = append(want.Ask, p.Ask...)
	}
	for k, v := range m.Claude.Env {
		want.Env[k] = v
	}
	// A deny always wins over an allow for the same entry.
	want.Allow = minus(want.Allow, want.Deny)
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
	perms, _ := next["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	var owned Owned
	mergeList := func(key string, prevOwned, desired []string) []string {
		cur := toStrings(perms[key])
		kept := minus(cur, prevOwned)
		merged := dedupe(append(kept, desired...))
		if len(merged) == 0 {
			delete(perms, key)
		} else {
			perms[key] = toAny(merged)
		}
		// Only entries tq added (not ones the user already had) are owned.
		return minus(desired, kept)
	}
	owned.Allow = mergeList("allow", prev.Allow, want.Allow)
	owned.Deny = mergeList("deny", prev.Deny, want.Deny)
	owned.Ask = mergeList("ask", prev.Ask, want.Ask)
	if want.DefaultMode != "" {
		perms["defaultMode"] = want.DefaultMode
	}
	next["permissions"] = perms
	// tq manages the permission posture; this flag only hides the bypass warning.
	delete(next, "skipDangerousModePermissionPrompt")

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
	if dryRun || !res.Changed {
		return res, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, err
	}
	if err := bundle.WriteJSONAtomic(path, next); err != nil {
		return res, err
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
	if fmt.Sprint(cp["defaultMode"]) != fmt.Sprint(np["defaultMode"]) {
		out = append(out, fmt.Sprintf("permissions.defaultMode: %v -> %v", orNone(cp["defaultMode"]), np["defaultMode"]))
	}
	for _, k := range []string{"allow", "deny", "ask"} {
		add := minus(toStrings(np[k]), toStrings(cp[k]))
		rm := minus(toStrings(cp[k]), toStrings(np[k]))
		if len(add) > 0 {
			out = append(out, fmt.Sprintf("permissions.%s: +%d", k, len(add)))
		}
		if len(rm) > 0 {
			out = append(out, fmt.Sprintf("permissions.%s: -%d (%s)", k, len(rm), strings.Join(rm, ", ")))
		}
	}
	if _, had := cur["skipDangerousModePermissionPrompt"]; had {
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

func orNone(v any) any {
	if v == nil {
		return "(unset)"
	}
	return v
}

func loadOwned(dir string) Owned {
	var o Owned
	raw, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err == nil {
		_ = json.Unmarshal(raw, &o)
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

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
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
