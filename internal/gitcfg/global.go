package gitcfg

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Run is a git runner: it executes git with the given arguments and returns
// trimmed output. RunGit is the real one; tests inject fakes.
type Run = func(args ...string) (string, error)

// IncludeIf is one `[includeIf "<Cond>"] path = <Path>` block of the global git
// config. Cond is the raw condition git stores as the subsection name, e.g.
// `gitdir:C:/repos/dirtybird/` (case-sensitive) or `gitdir/i:C:/repos/x/`.
type IncludeIf struct {
	Cond string
	Path string
}

// ListIncludeIf returns the includeIf blocks written directly in the user's
// global config (~/.gitconfig).
//
// `git config --global` does not expand include directives (git only respects
// them when searching all config files), so entries defined in an included
// file normally do not show up at all. --show-origin makes that guarantee
// explicit: any entry whose origin is tq's own managed include file is dropped,
// because those blocks are tq's output, not user drift, and `git config
// --global --unset` could not remove them anyway.
//
// The query is run with --null. Both an includeIf condition and a path may
// contain spaces, which makes git's default `<key> <value>` line ambiguous,
// and --show-origin C-quotes any origin holding a backslash (every Windows
// path). The NUL form has neither problem: git emits
// `<origin>\0<key>\n<value>\0` per entry. A runner faked for tests must
// produce that same shape.
func ListIncludeIf(run Run) ([]IncludeIf, error) {
	out, err := run("config", "--global", "--show-origin", "--null", "--get-regexp", `^includeif\.`)
	if err != nil {
		if isUnsetKey(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("git config --global --get-regexp ^includeif.: %w (%s)", err, strings.TrimSpace(out))
	}
	managed := normalize(IncludeFile())
	var res []IncludeIf
	origin := ""
	for _, rec := range strings.Split(out, "\x00") {
		nl := strings.IndexByte(rec, '\n')
		if nl < 0 {
			// A field without a value separator is the origin of the entry
			// that follows it.
			origin = strings.TrimPrefix(strings.TrimSpace(rec), "file:")
			continue
		}
		key := strings.TrimSpace(rec[:nl])
		val := strings.TrimSpace(rec[nl+1:])
		here := origin
		origin = ""
		if here != "" && normalize(here) == managed {
			continue
		}
		// git lowercases the section and the trailing key but preserves the
		// subsection (the condition) verbatim. Only `.path` is meaningful.
		if !strings.HasSuffix(key, ".path") || !strings.HasPrefix(strings.ToLower(key), "includeif.") {
			continue
		}
		cond := strings.TrimSuffix(key[len("includeif."):], ".path")
		if cond == "" {
			continue
		}
		res = append(res, IncludeIf{Cond: cond, Path: val})
	}
	return res, nil
}

// ListIncludes returns the global config's `include.path` values, in file
// order and exactly as git stores them (a leading `~` is not expanded — use
// ExpandPath for that).
func ListIncludes(run Run) ([]string, error) {
	out, err := run("config", "--global", "--get-all", "include.path")
	if err != nil {
		if isUnsetKey(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("git config --global --get-all include.path: %w (%s)", err, strings.TrimSpace(out))
	}
	var res []string
	for _, line := range strings.Split(out, "\n") {
		if v := strings.TrimSpace(strings.TrimSuffix(line, "\r")); v != "" {
			res = append(res, v)
		}
	}
	return res, nil
}

// GetGlobal reads one key from the global config. A key that is unset — or set
// to the empty string, which is indistinguishable through a fake runner and
// equally useless — reports present=false with a nil error.
func GetGlobal(run Run, key string) (string, bool, error) {
	out, err := run("config", "--global", "--get", key)
	if err != nil {
		if isUnsetKey(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("git config --global --get %s: %w (%s)", key, err, strings.TrimSpace(out))
	}
	v := strings.TrimSpace(out)
	return v, v != "", nil
}

// ExpandPath turns a git config path value into one the OS can stat: a leading
// `~` becomes the user's home directory (git expands it, os.Stat does not) and
// separators are made native.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, strings.TrimLeft(p[1:], `/\`))
		}
	}
	return filepath.FromSlash(p)
}

// isUnsetKey reports whether a git failure just means "the key is not set":
// git config exits 1 when a lookup finds nothing.
func isUnsetKey(err error) bool { return gitExitCode(err) == 1 }

// gitExitCode returns the process exit code of a git failure, or -1 when the
// error did not come from a finished process (a fake runner, git missing).
func gitExitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// normalize makes two path spellings comparable: forward slashes, cleaned,
// case-folded on Windows.
func normalize(p string) string {
	p = filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
