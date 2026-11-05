package trust

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
)

// Covered lists the keys (Key) of what the integrity record of workspace
// name covers (PRD v2 §8.3: the hook scripts, the shims and the manifest;
// the owner's decisions of 2026-11-05), sorted:
//
//   - the protected manifest, $TQ_HOME/identities/<name>/manifest.yaml;
//   - the root marker <root>/.tentaqles.yaml, root being <base>/<name> for
//     the first registered base that has that folder (in the old layout
//     this is the manifest itself);
//   - "settings.json#tq": the keys of the identity's Claude settings.json
//     that decide what runs (enforcingSettingsKeys), not the rest of the
//     file, which Claude Code rewrites for a theme or a model;
//   - every file under hooks/, scripts/ and .claude-plugin/ of the
//     tentaqles plugin installed in the identity's Claude config dir, the
//     highest installed version (plugin/CLAUDE.md);
//   - the identity's Codex hooks.json and "config.toml#tq", the block of
//     config.toml tq owns;
//   - every file under $TQ_HOME/shims/ when it exists.
//
// The tq binary is not covered: Phase 3's shim folder decides which tq the
// shell runs, and the binary is the OS's to protect.
//
// A path that does not exist is still covered: the record stores it as
// absent, and it appearing later is a change.
func Covered(name string) []string {
	var out []string
	out = append(out, Key(paths.ManifestPath(name)))
	if root := workspaceRoot(name); root != "" {
		out = append(out, Key(filepath.Join(root, manifest.FileName)))
	}
	claude := paths.IdentityDir(name, "claude")
	out = append(out, Key(filepath.Join(claude, "settings.json")+blockSuffix))
	if pr := PluginRoot(claude); pr != "" {
		for _, sub := range []string{"hooks", "scripts", ".claude-plugin"} {
			for _, f := range filesUnder(filepath.Join(pr, sub)) {
				out = append(out, Key(f))
			}
		}
	}
	codex := paths.IdentityDir(name, "codex")
	out = append(out, Key(filepath.Join(codex, "hooks.json")), Key(filepath.Join(codex, "config.toml")+blockSuffix))
	for _, f := range filesUnder(filepath.Join(paths.Home(), "shims")) {
		out = append(out, Key(f))
	}
	sort.Strings(out)
	return dedupe(out)
}

// pluginKey is the key of name's covered plugin folder, "" when none.
func pluginKey(name string) string {
	if pr := PluginRoot(paths.IdentityDir(name, "claude")); pr != "" {
		return Key(pr)
	}
	return ""
}

// workspaceRoot is <base>/<name> for the first registered base that holds
// that folder, "" when none does (or the registry cannot be read).
func workspaceRoot(name string) string {
	cfg, err := registry.Load()
	if err != nil {
		return ""
	}
	for _, base := range cfg.Bases {
		root := filepath.Join(base, name)
		if fi, err := os.Stat(root); err == nil && fi.IsDir() {
			return root
		}
	}
	return ""
}

// PluginRoot is the folder of the tentaqles plugin installed in claudeDir
// (a Claude config dir): <claudeDir>/plugins/cache/<marketplace>/tentaqles/
// <version>/ with a .claude-plugin/plugin.json, the highest version when
// several are installed (Claude Code keeps old versions in the cache, and
// a text sort puts 0.10.0 before 0.9.0). "" when none is installed.
func PluginRoot(claudeDir string) string {
	matches, _ := filepath.Glob(filepath.Join(claudeDir, "plugins", "cache", "*", "tentaqles", "*", ".claude-plugin", "plugin.json"))
	best := ""
	var bestV []int
	for _, m := range matches {
		dir := filepath.Dir(filepath.Dir(m))
		v := parseVersion(filepath.Base(dir))
		if best == "" || lessVersion(bestV, v) || (!lessVersion(v, bestV) && dir > best) {
			best, bestV = dir, v
		}
	}
	return best
}

// parseVersion reads the numeric parts of a version folder name
// ("0.16.0", "1.2.3-rc1" → 1 2 3); a part that does not start with a
// number counts as 0.
func parseVersion(s string) []int {
	s = strings.TrimPrefix(s, "v")
	var out []int
	for _, part := range strings.Split(s, ".") {
		n := 0
		digits := part
		if i := strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
			digits = part[:i]
		}
		if digits != "" {
			n, _ = strconv.Atoi(digits)
		}
		out = append(out, n)
	}
	return out
}

func lessVersion(a, b []int) bool {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// filesUnder lists every entry under dir recursively (regular files, and
// anything else as a path whose hash is irregular), sorted; nothing when
// dir does not exist. Symlinked directories are not followed.
func filesUnder(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return nil
		}
		if !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func dedupe(xs []string) []string {
	out := xs[:0]
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}
