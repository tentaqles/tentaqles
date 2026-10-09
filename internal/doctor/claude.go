package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/claudesettings"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// unpinnedRe matches an npx/bunx/uvx invocation of a package with no version
// or with @latest: it re-resolves on every start, so a compromised release
// runs immediately.
var unpinnedRe = regexp.MustCompile(`(?i)\b(npx|bunx|pnpm\s+dlx|uvx)\b[^"]*?(@latest\b|\s-y\s+(@?[a-z0-9._-]+/)?[a-z0-9._-]+(\s|"|$))`)

// claudeChecks reports identity-level Claude config problems for one
// trusted workspace: settings drift from `tq settings render`, and unpinned
// packages in settings.json / .claude.json.
func claudeChecks(w *resolve.Workspace, add func(level, code, ws, msg, fix string)) {
	dir := paths.IdentityDir(w.Name, "claude")
	if _, err := os.Stat(dir); err != nil {
		return
	}
	want := claudesettings.Desired(w.Manifest, trust.IsBypassAllowed(w.Hash), "")
	if res, err := claudesettings.Render(dir, want, true); err == nil && res.Changed {
		add("warn", "settings-drift", w.Name, "settings.json differs from the tq baseline: "+strings.Join(res.Changes, "; "), "tq settings render "+w.Name)
	}
	for _, f := range []string{"settings.json", ".claude.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		for _, cmd := range commandStrings(raw) {
			if m := unpinnedRe.FindString(cmd); m != "" {
				add("warn", "unpinned-package", w.Name, fmt.Sprintf("%s runs an unpinned package (%s)", f, strings.TrimSpace(m)), "pin an exact version, e.g. pkg@1.2.3")
			}
		}
	}
}

// commandStrings returns "command + args" for every statusLine, hook and
// MCP server entry in a settings-like JSON document.
func commandStrings(raw []byte) []string {
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if c, ok := x["command"].(string); ok {
				s := c
				if args, ok := x["args"].([]any); ok {
					for _, a := range args {
						if as, ok := a.(string); ok {
							s += " " + as
						}
					}
				}
				out = append(out, s)
			}
			for _, vv := range x {
				walk(vv)
			}
		case []any:
			for _, vv := range x {
				walk(vv)
			}
		}
	}
	walk(doc)
	sort.Strings(out)
	return dedupeStrings(out)
}

func dedupeStrings(xs []string) []string {
	var out []string
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// pluginSkew reports a plugin installed at different versions across
// identities (e.g. one identity still on an old tentaqles plugin).
func pluginSkew(names []string, add func(level, code, ws, msg, fix string)) {
	versions := map[string]map[string][]string{} // plugin -> version -> identities
	for _, n := range names {
		raw, err := os.ReadFile(filepath.Join(paths.IdentityDir(n, "claude"), "plugins", "installed_plugins.json"))
		if err != nil {
			continue
		}
		var doc struct {
			Plugins map[string][]struct {
				Version string `json:"version"`
			} `json:"plugins"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		for p, insts := range doc.Plugins {
			for _, in := range insts {
				if versions[p] == nil {
					versions[p] = map[string][]string{}
				}
				versions[p][in.Version] = append(versions[p][in.Version], n)
			}
		}
	}
	var plugins []string
	for p := range versions {
		plugins = append(plugins, p)
	}
	sort.Strings(plugins)
	for _, p := range plugins {
		vs := versions[p]
		if len(vs) < 2 {
			continue
		}
		var parts []string
		for v, ids := range vs {
			sort.Strings(ids)
			parts = append(parts, v+" ("+strings.Join(ids, ",")+")")
		}
		sort.Strings(parts)
		add("warn", "plugin-version-skew", "", p+" differs across identities: "+strings.Join(parts, "; "), "claude plugin update "+p+" (per identity)")
	}
}

// pythonStoreAlias reports when python3 resolves to the Microsoft Store
// alias, which adds ~150ms to every Python hook start.
func pythonStoreAlias(lookPath func(string) (string, error), goos string, add func(level, code, ws, msg, fix string)) {
	if goos != "windows" || lookPath == nil {
		return
	}
	if p, err := lookPath("python3"); err == nil && strings.Contains(strings.ToLower(p), `\windowsapps\`) {
		if os.Getenv("TENTAQLES_PY") == "" {
			add("warn", "python-store-alias", "", "python3 is the Microsoft Store alias ("+p+"); Python hooks start ~150ms slower", "set TENTAQLES_PY to a python.org interpreter, or disable the alias under Settings > Apps > App execution aliases")
		}
	}
}
