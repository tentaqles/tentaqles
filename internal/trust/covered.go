package trust

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
)

// pluginSkipTop are the folders of the plugin root that run nothing on
// their own: skills and agents are prompts, templates are documents, tests
// are the plugin's own suite. __pycache__ is skipped at any depth (Python
// rewrites it, and tq_env.sh keeps Python from reading it). Everything else
// of the loaded plugin is covered: hooks/, scripts/, the tentaqles Python
// package, .claude-plugin/, settings.json, and any file that should not be
// there (a bin/ the plugin never ships).
var pluginSkipTop = map[string]bool{"skills": true, "agents": true, "templates": true, "tests": true}

// coverage is what the record of one workspace covers, computed once per
// memo scope.
type coverage struct {
	keys []string
	// plugin is the key of the plugin root covered, "" when none.
	plugin string
	// installed is the key of installed_plugins.json#tq.
	installed string
}

// Covered lists the keys (Key) of what the integrity record of workspace
// name covers (PRD v2 §8.3: the hook scripts, the shims and the manifest;
// the owner's decisions of 2026-11-05), sorted:
//
//   - the protected manifest, $TQ_HOME/identities/<name>/manifest.yaml;
//   - the root marker <root>/.tentaqles.yaml, root being <base>/<name> for
//     the first registered base that has that folder (in the old layout
//     this is the manifest itself);
//   - "settings.json#tq": the keys of the identity's Claude settings.json
//     that decide what runs (enforcingSettingsKey), not the rest of the
//     file, which Claude Code rewrites for a theme or a model;
//   - "plugins/installed_plugins.json#tq": where Claude Code loads the
//     tentaqles plugin from and its version;
//   - every file of the loaded tentaqles plugin's folder (PluginRoot)
//     outside skills/, agents/, templates/, tests/ and __pycache__;
//   - every file under $TQ_HOME/shims/<name>/ and directly under $TQ_HOME/shims/ when they
//     exist (ADR-0016: one shims folder per identity).
//
// The tq binary on PATH is not covered: Phase 3's shim folder decides which
// tq the shell runs, and the binary is the OS's to protect.
//
// Each root (the manifest's folder, the workspace root, the Claude
// identity dir, the plugin folder, the shims folder) is resolved
// once (Key) and the files under it are keyed by joining their relative
// path, so the links of a root are followed and a link inside it is a
// change. A path that does not exist is still covered: the record stores it
// as absent, and it appearing later is a change.
func Covered(name string) []string { return coverageOf(name).keys }

// pluginKey is the key of name's covered plugin folder, "" when none.
func pluginKey(name string) string { return coverageOf(name).plugin }

func coverageOf(name string) coverage {
	if c, ok := memoCoverage(name); ok {
		return c
	}
	c := computeCoverage(name)
	storeCoverage(name, c)
	return c
}

func computeCoverage(name string) coverage {
	var c coverage
	add := func(root, rel string) string {
		k := joinKey(root, rel)
		c.keys = append(c.keys, k)
		return k
	}
	// Two full resolutions (the state folder and the workspace root); every
	// other root is derived from its parent (keyUnder).
	home := paths.Home()
	homeKey := Key(home)
	idDir := filepath.Join(paths.IdentitiesRoot(), name)
	id := keyUnder(homeKey, home, idDir)
	add(id, "manifest.yaml")
	if root := workspaceRoot(name); root != "" {
		add(Key(root), manifest.FileName)
	}
	claudeDir := paths.IdentityDir(name, "claude")
	claude := keyUnder(id, idDir, claudeDir)
	add(claude, "settings.json"+blockSuffix)
	c.installed = add(claude, "plugins/installed_plugins.json"+blockSuffix)
	if pr := PluginRoot(claudeDir); pr != "" {
		c.plugin = keyUnder(claude, claudeDir, pr)
		for _, rel := range pluginFiles(pr) {
			add(c.plugin, rel)
		}
	}
	// The shims of ADR-0016 live one folder per identity; an identity's record covers its own
	// folder, so writing client A's shims does not change client B's record. A file directly
	// under shims/ (the flat layout of an older tq) is covered for every identity.
	shimsDir := filepath.Join(home, "shims")
	ownShims := func(rel string, d fs.DirEntry) bool {
		return d.IsDir() && !strings.Contains(rel, "/") && !strings.EqualFold(d.Name(), name)
	}
	if rels := relFilesUnder(shimsDir, ownShims); len(rels) > 0 {
		shims := keyUnder(homeKey, home, shimsDir)
		for _, rel := range rels {
			add(shims, rel)
		}
	}
	sort.Strings(c.keys)
	c.keys = dedupe(c.keys)
	return c
}

// keyUnder is the key of dir, a folder under base whose key is baseKey: the
// relative path joined to baseKey, after one Lstat per folder between them
// shows none of them is a link (a symlink, or on Windows a junction, which
// Lstat reports as irregular); otherwise, or when dir is not under base,
// Key(dir), which resolves the links. A folder that does not exist ends the
// check: nothing below it can be a link. The result equals Key(dir).
func keyUnder(baseKey, base, dir string) string {
	rel, err := filepath.Rel(base, dir)
	if err != nil || paths.IsOutside(rel) {
		return Key(dir)
	}
	if rel == "." {
		return baseKey
	}
	cur := base
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			if absent(err) {
				break
			}
			return Key(dir)
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !fi.IsDir() {
			return Key(dir)
		}
	}
	return joinKey(baseKey, rel)
}

// joinKey is the key of rel (forward slashes) under the root key root.
func joinKey(root, rel string) string {
	k := root + "/" + filepath.ToSlash(rel)
	if runtime.GOOS == "windows" {
		k = strings.ToLower(k)
	}
	return k
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

// isPluginDir reports a folder with .claude-plugin/plugin.json.
func isPluginDir(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil && fi.Mode().IsRegular()
}

// PluginRoot is the folder of the tentaqles plugin Claude Code loads for the
// Claude config dir claudeDir: the installPath its
// plugins/installed_plugins.json names for tentaqles (the user-scope entry
// first), when that folder holds .claude-plugin/plugin.json; otherwise the
// highest version under <claudeDir>/plugins/cache/<marketplace>/tentaqles/
// (Claude Code keeps old versions in the cache, and a text sort puts 0.10.0
// before 0.9.0). "" when none is installed.
func PluginRoot(claudeDir string) string {
	if p := installedRoot(claudeDir); p != "" {
		return p
	}
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

// installedRoot is the installPath of tentaqles in claudeDir's
// installed_plugins.json, "" when there is none that is a plugin folder.
func installedRoot(claudeDir string) string {
	entries, ok, err := readInstalled(filepath.Join(claudeDir, "plugins", "installed_plugins.json"))
	if err != nil || !ok {
		return ""
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Scope == "user" && entries[j].Scope != "user" })
	for _, e := range entries {
		if e.InstallPath != "" && filepath.IsAbs(e.InstallPath) && isPluginDir(e.InstallPath) {
			return filepath.Clean(e.InstallPath)
		}
	}
	return ""
}

// pluginFiles lists the covered files of the plugin folder root, relative,
// forward slashes, sorted.
func pluginFiles(root string) []string {
	return relFilesUnder(root, func(rel string, d fs.DirEntry) bool {
		if !d.IsDir() {
			return false
		}
		if d.Name() == "__pycache__" {
			return true
		}
		return !strings.Contains(rel, "/") && pluginSkipTop[d.Name()]
	})
}

// relFilesUnder lists every entry under dir that is not a directory
// (regular files, and anything else as a path whose hash is irregular),
// relative to dir with forward slashes, sorted; nothing when dir does not
// exist. skip, when set, prunes a directory. Symlinked directories are not
// followed.
func relFilesUnder(dir string, skip func(rel string, d fs.DirEntry) bool) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if skip != nil && skip(rel, d) {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
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

func dedupe(xs []string) []string {
	out := xs[:0]
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}
