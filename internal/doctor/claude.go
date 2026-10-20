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
// identities (e.g. one identity still on an old tentaqles plugin). Identities
// whose plugins dirs are links to one shared directory share one file; it is
// counted once, under the first identity that names it.
func pluginSkew(names []string, add func(level, code, ws, msg, fix string)) {
	versions := map[string]map[string][]string{} // plugin -> version -> identities
	var counted []os.FileInfo
	for _, n := range names {
		file := filepath.Join(paths.IdentityDir(n, "claude"), "plugins", "installed_plugins.json")
		fi, err := os.Stat(file)
		if err != nil {
			continue
		}
		dup := false
		for _, c := range counted {
			if os.SameFile(c, fi) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		counted = append(counted, fi)
		raw, err := os.ReadFile(file)
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

// isLink reports whether p is a symlink or a Windows junction. Since Go 1.23
// a junction is ModeIrregular rather than ModeSymlink and EvalSymlinks does
// not follow it, so the two plugin checks compare file identity instead of
// resolved paths.
func isLink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

// linkDest is where a link points, for the message; best effort.
func linkDest(p string) string {
	if t, err := os.Readlink(p); err == nil && t != "" {
		return t
	}
	if t, err := filepath.EvalSymlinks(p); err == nil {
		return t
	}
	return "another directory"
}

// installedUnder reports whether installPath lies in the plugins dir
// described by pluginsFi: each ancestor (a few levels, since records end in
// cache/<marketplace>/<plugin>/<version>) is compared by file identity, so a
// path spelled through a link, a junction or a different case still counts
// as the same directory.
func installedUnder(installPath string, pluginsFi os.FileInfo) bool {
	dir := filepath.Clean(installPath)
	for i := 0; i < 8; i++ {
		if fi, err := os.Stat(dir); err == nil && os.SameFile(fi, pluginsFi) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
	return false
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

// pluginForeignPath reports plugins whose install record points outside the
// identity's own plugins dir: into another identity's plugin cache (typically
// a config dir that was copied from a sibling identity) or into the default
// ~/.claude. Hooks and skills then run another identity's files, and updating
// or removing the plugin there silently breaks this identity (seen as
// SessionStart hooks failing on a path under a different identity). One
// finding per identity and source keeps the report readable.
//
// An identity whose plugins dir is itself a link to a shared directory gets a
// single plugins-dir-linked finding instead: every record in that shared file
// is "its own" (file identity, not spelling), and reinstalling would only
// rewrite the shared file for everyone sharing it.
func pluginForeignPath(names []string, add func(level, code, ws, msg, fix string)) {
	root := strings.TrimRight(normPath(paths.IdentitiesRoot()), "/")
	type pdir struct {
		name, path string
		fi         os.FileInfo
	}
	var dirs []pdir
	for _, n := range names {
		p := filepath.Join(paths.IdentityDir(n, "claude"), "plugins")
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			dirs = append(dirs, pdir{n, p, fi})
		}
	}
	for _, d := range dirs {
		if isLink(d.path) {
			var others []string
			for _, o := range dirs {
				if o.name != d.name && os.SameFile(o.fi, d.fi) {
					others = append(others, o.name)
				}
			}
			sort.Strings(others)
			with := ""
			if len(others) > 0 {
				with = ", shared with " + strings.Join(others, ", ")
			}
			add("warn", "plugins-dir-linked", d.name,
				"plugins dir is a link to "+linkDest(d.path)+with+": every plugin install from any identity using it rewrites the same installed_plugins.json and cache",
				"give the identity its own plugins dir (replace the link with a real directory), then tq bundle sync "+d.name)
		}
		raw, err := os.ReadFile(filepath.Join(d.path, "installed_plugins.json"))
		if err != nil {
			continue
		}
		var doc struct {
			Plugins map[string][]struct {
				Version     string `json:"version"`
				InstallPath string `json:"installPath"`
			} `json:"plugins"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		own := strings.TrimRight(normPath(d.path), "/")
		var keys []string
		for p := range doc.Plugins {
			keys = append(keys, p)
		}
		sort.Strings(keys)
		// source ("" = outside every identity, e.g. ~/.claude) -> plugins
		groups := map[string][]string{}
		for _, p := range keys {
			for _, in := range doc.Plugins[p] {
				if in.InstallPath == "" {
					continue
				}
				if installedUnder(in.InstallPath, d.fi) {
					continue
				}
				ip := normPath(in.InstallPath)
				if strings.HasPrefix(ip, own+"/") {
					continue // the record's dir is gone; by spelling it is ours
				}
				src := ""
				if strings.HasPrefix(ip, root+"/") {
					if other := strings.SplitN(strings.TrimPrefix(ip, root+"/"), "/", 2)[0]; other != "" && other != d.name {
						src = other
					}
				}
				groups[src] = append(groups[src], p+" "+in.Version)
			}
		}
		var srcs []string
		for src := range groups {
			if src != "" {
				srcs = append(srcs, src)
			}
		}
		sort.Strings(srcs)
		if _, ok := groups[""]; ok {
			srcs = append(srcs, "")
		}
		for _, src := range srcs {
			where := "outside this identity's config dir (e.g. ~/.claude)"
			if src != "" {
				where = "the " + src + " identity's plugin cache"
			}
			list := groups[src]
			add("warn", "plugin-foreign-path", d.name,
				fmt.Sprintf("%d plugin(s) installed from %s: %s", len(list), where, strings.Join(list, ", ")),
				"reinstall each inside the identity: tq run "+d.name+" -- claude plugin uninstall <plugin>, then tq run "+d.name+" -- claude plugin install <plugin>")
		}
	}
}
