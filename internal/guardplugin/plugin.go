package guardplugin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

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
// (Claude Code keeps several versions in the cache, compared by number:
// a text sort would put 1.10.0 before 1.9.0). "" when none is installed.
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

// installedEntry is what installed_plugins.json#tq keeps of one install of
// the tentaqles plugin: where Claude Code loads it from, and its version.
type installedEntry struct {
	Plugin      string `json:"plugin"`
	InstallPath string `json:"installPath"`
	Version     string `json:"version"`
	// Scope picks the entry PluginRoot uses (user first); not hashed.
	Scope string `json:"-"`
}

// readInstalled reads the tentaqles entries of a Claude config dir's
// plugins/installed_plugins.json, sorted. ok is false when the file is not
// a JSON object with a plugins map.
func readInstalled(file string) (entries []installedEntry, ok bool, err error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		if absent(err) {
			return nil, true, nil
		}
		return nil, false, err
	}
	var doc struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
			Version     string `json:"version"`
			Scope       string `json:"scope"`
		} `json:"plugins"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, false, nil
	}
	for name, insts := range doc.Plugins {
		if !strings.HasPrefix(name, "tentaqles@") {
			continue
		}
		for _, in := range insts {
			entries = append(entries, installedEntry{name, in.InstallPath, in.Version, in.Scope})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Plugin != entries[j].Plugin {
			return entries[i].Plugin < entries[j].Plugin
		}
		return entries[i].InstallPath < entries[j].InstallPath
	})
	return entries, true, nil
}

// absent reports a stat error that means "nothing there": the file, or a
// directory on its path, does not exist (or a path component is a file).
func absent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}
