package gitcfg

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// RemoveIncludeIf drops one includeIf block from the global config. The value
// is matched as an anchored, escaped regex so a path that is a prefix of
// another one cannot be removed by accident. The now-empty `[includeIf "..."]`
// section header is removed too, but only when no other key survives in it.
//
// Removing a block that is not there is a no-op, so the call is idempotent.
func RemoveIncludeIf(run Run, inc IncludeIf) error {
	if strings.TrimSpace(inc.Cond) == "" {
		return errors.New("gitcfg: RemoveIncludeIf: empty condition")
	}
	key := "includeIf." + inc.Cond + ".path"
	// --unset-all rather than --unset: a hand-edited config can hold the same
	// path twice, and --unset fails with "multiple values" in that case.
	args := []string{"config", "--global", "--unset-all", key}
	if inc.Path != "" {
		args = append(args, "^"+regexp.QuoteMeta(inc.Path)+"$")
	}
	if out, err := run(args...); err != nil && !isNothingToUnset(err) {
		return fmt.Errorf("git config --global --unset-all %s: %w (%s)", key, err, strings.TrimSpace(out))
	}
	left, err := run("config", "--global", "--get-regexp", "^"+regexp.QuoteMeta("includeif."+inc.Cond+"."))
	if err == nil && strings.TrimSpace(left) != "" {
		return nil // other keys remain: keep the section
	}
	// The section may already be gone; git then exits non-zero, which is fine.
	_, _ = run("config", "--global", "--remove-section", "includeIf."+inc.Cond)
	return nil
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

// RemoveInclude drops one `include.path` value from the global config, matched
// as an anchored, escaped regex. Removing an absent value is a no-op.
func RemoveInclude(run Run, path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("gitcfg: RemoveInclude: empty path")
	}
	out, err := run("config", "--global", "--unset-all", "include.path", "^"+regexp.QuoteMeta(path)+"$")
	if err != nil && !isNothingToUnset(err) {
		return fmt.Errorf("git config --global --unset-all include.path %s: %w (%s)", path, err, strings.TrimSpace(out))
	}
	return nil
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

// UnsetGlobal removes every value of key from the global config. Unsetting a
// key that is not there is a no-op.
func UnsetGlobal(run Run, key string) error {
	out, err := run("config", "--global", "--unset-all", key)
	if err != nil && !isNothingToUnset(err) {
		return fmt.Errorf("git config --global --unset-all %s: %w (%s)", key, err, strings.TrimSpace(out))
	}
	return nil
}

// ParseUserSection reads name and email out of a git config file's `[user]`
// section without shelling out to git — the file may be one git is not
// currently including, and migrate needs the values the user actually wrote.
//
// It is a deliberately minimal INI parse: `[user]` only (a `[user "sub"]`
// subsection is a different section and is ignored), section and key names
// compared case-insensitively, `#`/`;` comments and surrounding quotes
// stripped. Missing keys come back empty; a missing section is not an error.
func ParseUserSection(file string) (string, string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", "", fmt.Errorf("gitcfg: reading %s: %w", file, err)
	}
	var name, email string
	inUser := false
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			end := strings.Index(line, "]")
			if end < 0 {
				inUser = false
				continue
			}
			inUser = strings.EqualFold(strings.TrimSpace(line[1:end]), "user")
			continue
		}
		if !inUser {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(line[:eq])) {
		case "name":
			name = configValue(line[eq+1:])
		case "email":
			email = configValue(line[eq+1:])
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", fmt.Errorf("gitcfg: reading %s: %w", file, err)
	}
	return name, email, nil
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

// configValue strips a trailing unquoted comment and the quotes git uses
// around values with leading/trailing spaces.
func configValue(v string) string {
	var b strings.Builder
	quoted := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '\\' && quoted && i+1 < len(v):
			i++
			switch v[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(v[i])
			}
		case c == '"':
			quoted = !quoted
		case (c == '#' || c == ';') && !quoted:
			return strings.TrimSpace(b.String())
		default:
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String())
}

// isUnsetKey reports whether a git failure just means "the key is not set":
// git config exits 1 when a lookup finds nothing.
func isUnsetKey(err error) bool { return gitExitCode(err) == 1 }

// isNothingToUnset reports whether an --unset-all failure just means there was
// nothing to remove. git config exits 5 for "you try to unset an option which
// does not exist"; 1 covers the same case for lookups.
func isNothingToUnset(err error) bool {
	c := gitExitCode(err)
	return c == 1 || c == 5
}

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
