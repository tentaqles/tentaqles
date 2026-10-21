package guard

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// This file holds the shared git-invocation reader used by every commit and
// push rule (commit-secret, commit-pii, push-pii, commit-message, text-pii)
// and the shared directory resolver (which repository a command acts on).
//
// Two passes find git calls:
//   - a quote-aware pass splits on separators outside quotes (a `$(` or a
//     backtick inside double quotes still starts a new command, because the
//     shell runs it), and splits words honouring quotes, so the arguments are
//     exact (`-m "feat(api): x" file.csv` keeps file.csv);
//   - the older naive pass splits on every separator, quotes or not. A git
//     call it finds that the first pass did not (one inside a quoted string,
//     such as `sh -c 'git commit'`) is reported with Hidden set: it may run,
//     but its arguments and directory cannot be trusted.

// gitWrappers are words that run the next word as the command.
var gitWrappers = map[string]bool{"env": true, "command": true, "exec": true, "sudo": true, "doas": true,
	"nice": true, "nohup": true, "time": true, "&": true}

// gitWrapperValueFlags take a separate value after a wrapper (sudo -u bob,
// nice -n 5, env -u NAME).
var gitWrapperValueFlags = map[string]bool{"-u": true, "-g": true, "-n": true, "-p": true, "-h": true, "-U": true, "-D": true, "-R": true, "-T": true}

// gitGlobalValueOpts are git's global options that take a separate value.
var gitGlobalValueOpts = map[string]bool{"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
	"--namespace": true, "--super-prefix": true, "--config-env": true}

var envAssignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// IsGitWord reports whether a command word names git: git, git.exe, a path
// ending in /git, /git.exe or \git.exe, or any of these quoted.
func IsGitWord(w string) bool {
	w = strings.Trim(w, `"'`)
	w = strings.ReplaceAll(w, `\`, "/")
	w = w[strings.LastIndex(w, "/")+1:]
	w = strings.ToLower(w)
	return w == "git" || w == "git.exe"
}

func isGitDirEnv(assign string) bool {
	name := strings.ToUpper(assign[:strings.Index(assign, "=")])
	return name == "GIT_DIR" || name == "GIT_WORK_TREE"
}

// ParseGitWords reads one simple command, already split into words, as a git
// call: leading NAME=value assignments and wrappers (env, command, exec, sudo,
// doas, nice, nohup, time, the PowerShell call operator &) are skipped, the
// command word is matched with IsGitWord, and git's global options are read up
// to the sub-command.
func ParseGitWords(f []string) (GitInvocation, bool) {
	var inv GitInvocation
	i := 0
	for i < len(f) {
		w := f[i]
		switch {
		case envAssignRe.MatchString(w):
			if isGitDirEnv(w) {
				inv.GitDirOverride = true
			}
			i++
			continue
		case gitWrappers[strings.ToLower(strings.Trim(w, `"'`))]:
			i++
			for i < len(f) && strings.HasPrefix(f[i], "-") && len(f[i]) > 1 {
				if f[i] == "-C" || strings.HasPrefix(f[i], "--chdir") {
					inv.envChdir = true // env -C dir: another directory
				}
				if gitWrapperValueFlags[f[i]] || f[i] == "-C" || f[i] == "--chdir" {
					i++
				}
				i++
			}
			continue
		}
		break
	}
	if i >= len(f) || !IsGitWord(f[i]) {
		return GitInvocation{}, false
	}
	for i++; i < len(f); i++ {
		a := f[i]
		switch {
		case gitGlobalValueOpts[a]:
			if i+1 < len(f) {
				switch a {
				case "-C":
					inv.Dirs = append(inv.Dirs, f[i+1])
				case "-c":
					inv.Configs = append(inv.Configs, f[i+1])
				case "--git-dir", "--work-tree":
					inv.GitDirOverride = true
				}
			}
			i++
		case strings.HasPrefix(a, "--git-dir=") || strings.HasPrefix(a, "--work-tree="):
			inv.GitDirOverride = true
		case strings.HasPrefix(a, "--config-env="):
			inv.Configs = append(inv.Configs, strings.TrimPrefix(a, "--config-env="))
		case strings.HasPrefix(a, "-"):
		default:
			inv.Sub = a
			inv.Args = f[i+1:]
			return inv, true
		}
	}
	return GitInvocation{}, false // bare git or only global options
}

// maskQuoted returns c with every separator character inside quotes replaced
// by a placeholder of the same length, so sepRegexp splits only on real
// separators. Inside double quotes `$(` and a backtick are left alone: the
// shell runs them as commands.
func maskQuoted(c string) string {
	b := []byte(c)
	var q byte
	for i := 0; i < len(b); i++ {
		ch := b[i]
		switch q {
		case 0:
			switch ch {
			case '\\':
				i++
			case '\'', '"':
				q = ch
			}
		case '\'':
			if ch == '\'' {
				q = 0
			} else if isSepByte(ch) {
				b[i] = 0x01
			}
		case '"':
			switch {
			case ch == '\\':
				i++
			case ch == '"':
				q = 0
			case ch == '$' && i+1 < len(b) && b[i+1] == '(':
				i++ // command substitution runs: keep `$(`
			case ch == '`':
			case isSepByte(ch):
				b[i] = 0x01
			}
		}
	}
	return string(b)
}

func isSepByte(ch byte) bool {
	switch ch {
	case '&', '|', ';', '\n', '\r', '(', ')', '{', '}', '`', '$':
		return true
	}
	return false
}

// splitWords splits a simple command into words, removing quotes ('…' is
// literal; "…" honours \" \\ \$ \`; a backslash outside quotes escapes the
// next character). An open quote runs to the end.
func splitWords(s string) []string {
	var out []string
	var cur strings.Builder
	in := false
	var q rune
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case q == '\'':
			if r == '\'' {
				q = 0
			} else {
				cur.WriteRune(r)
			}
		case q == '"':
			switch {
			case r == '"':
				q = 0
			case r == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]):
				i++
				cur.WriteRune(rs[i])
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			q, in = r, true
		case r == '\\' && i+1 < len(rs) && rs[i+1] != '\n':
			// a Windows path such as C:\x\git.exe keeps its backslashes;
			// only quotes, spaces and backslashes are escaped
			if strings.ContainsRune("\"' \\", rs[i+1]) {
				i++
			}
			cur.WriteRune(rs[i])
			in = true
		case r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == 0x01:
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}

type span struct{ start, end int }

func segmentSpans(c string) []span {
	var out []span
	prev := 0
	for _, loc := range sepRegexp.FindAllStringIndex(c, -1) {
		out = append(out, span{prev, loc[0]})
		prev = loc[1]
	}
	return append(out, span{prev, len(c)})
}

var cdWords = map[string]bool{"cd": true, "pushd": true, "chdir": true, "set-location": true, "sl": true, "push-location": true, "popd": true, "pop-location": true}

// cdTarget reads a cd-like command: ok reports that words is one, dir is its
// single directory argument ("" when it has none or more than one).
func cdTarget(words []string) (dir string, ok bool) {
	if len(words) == 0 || !cdWords[strings.ToLower(words[0])] {
		return "", false
	}
	var pos []string
	for i := 1; i < len(words); i++ {
		w := words[i]
		lw := strings.ToLower(w)
		switch {
		case lw == "-path" || lw == "-literalpath":
			if i+1 < len(words) {
				pos = append(pos, words[i+1])
				i++
			}
		case w == "--" || w == "-P" || w == "-L" || w == "-e" || w == "-@":
		case w == "-":
			return "", true // previous directory: unknown here
		case strings.HasPrefix(w, "-"):
			return "", true
		default:
			pos = append(pos, w)
		}
	}
	if len(pos) != 1 || strings.ToLower(words[0]) == "popd" || strings.ToLower(words[0]) == "pop-location" {
		return "", true
	}
	return pos[0], true
}

// gitSegments returns every git invocation found in command, in order, each
// annotated with the directory context (leading cd, later cd) it runs in.
func gitSegments(c string) []gitInvocation {
	var out []gitInvocation
	masked := maskQuoted(c)
	covered := map[int]bool{}
	base, baseUnknown := "", false
	for k, sp := range segmentSpans(masked) {
		text := c[sp.start:sp.end]
		words := splitWords(strings.ReplaceAll(masked[sp.start:sp.end], "\x01", "\x02"))
		if strings.Contains(masked[sp.start:sp.end], "\x01") {
			words = splitWords(text) // quoted separators are data: read the original text
		}
		if len(words) == 0 {
			continue
		}
		if dir, ok := cdTarget(words); ok {
			if k == 0 && dir != "" && !baseUnknown && base == "" {
				base = dir
			} else {
				baseUnknown = true
			}
			continue
		}
		inv, ok := ParseGitWords(words)
		if !ok {
			continue
		}
		covered[firstNonSpace(c, sp.start)] = true
		inv.Base, inv.BaseUnknown = base, baseUnknown
		out = append(out, gitInvocation{GitInvocation: inv, start: sp.start})
	}
	// naive pass: git calls hidden inside quoted strings
	naive := blankHeredocBodies(c)
	for _, sp := range segmentSpans(naive) {
		f := strings.Fields(naive[sp.start:sp.end])
		inv, ok := ParseGitWords(f)
		if !ok {
			continue
		}
		at := firstNonSpace(c, sp.start)
		if covered[at] {
			continue
		}
		inv.Hidden = true
		out = append(out, gitInvocation{GitInvocation: inv, start: sp.start})
	}
	sortByStart(out)
	return out
}

func firstNonSpace(c string, i int) int {
	for i < len(c) && (c[i] == ' ' || c[i] == '\t') {
		i++
	}
	return i
}

func sortByStart(s []gitInvocation) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].start < s[j-1].start; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// blankHeredocBodies replaces heredoc body lines with spaces (keeping the
// length and line breaks), quoted `<<` included: a body is data, never a
// command, even inside `"$(cat <<'EOF' … EOF)"`.
func blankHeredocBodies(c string) string {
	lines := strings.Split(c, "\n")
	for i := 0; i < len(lines); i++ {
		for _, loc := range heredocStart.FindAllStringSubmatchIndex(lines[i], -1) {
			word := lines[i][loc[4]:loc[5]]
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == word {
					for k := i + 1; k < j; k++ {
						lines[k] = strings.Repeat(" ", len(lines[k]))
					}
					i = j
					break
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// DirError says why the directory a command acts on could not be resolved.
type DirError struct{ Reason string }

func (e *DirError) Error() string { return e.Reason }

// IsDirError reports whether err is a DirError.
func IsDirError(err error) bool {
	var d *DirError
	return errors.As(err, &d)
}

var msysDriveRe = regexp.MustCompile(`^/([A-Za-z])(/|$)`)

// ResolvePath resolves p against base the way a shell on this machine would.
// It refuses what it cannot evaluate: ~, variables, substitutions, `-`.
func ResolvePath(base, p string) (string, error) {
	if p == "" || p == "-" || strings.HasPrefix(p, "~") || strings.ContainsAny(p, "$`%") {
		return "", &DirError{Reason: "the command changes to a directory tq cannot evaluate (" + describe(p) + ")"}
	}
	if runtime.GOOS == "windows" {
		if m := msysDriveRe.FindStringSubmatch(p); m != nil {
			p = strings.ToUpper(m[1]) + ":/" + strings.TrimPrefix(p[len(m[0]):], "/")
		}
	}
	p = filepath.FromSlash(p)
	switch {
	case filepath.IsAbs(p):
		return filepath.Clean(p), nil
	case runtime.GOOS == "windows" && (strings.HasPrefix(p, `\`) || filepath.VolumeName(p) != ""):
		return "", &DirError{Reason: "the command changes to a drive-relative directory (" + describe(p) + ")"}
	}
	return filepath.Join(base, p), nil
}

func describe(p string) string {
	if p == "" {
		return "no directory given"
	}
	return p
}

func existingDir(d string) error {
	fi, err := os.Stat(d)
	if err != nil || !fi.IsDir() {
		return &DirError{Reason: "the directory the command runs in does not exist: " + d}
	}
	return nil
}

// Dir returns the directory this git call acts on: cwd, then a leading
// `cd <dir>` (first segment only), then every -C in order. It fails when the
// directory cannot be known or does not exist: callers ask instead of
// scanning the wrong repository.
func (g GitInvocation) Dir(cwd string) (string, error) {
	switch {
	case g.Hidden:
		return "", &DirError{Reason: "a git call inside a quoted string or a nested shell; tq cannot tell which repository it acts on"}
	case g.BaseUnknown:
		return "", &DirError{Reason: "the command changes directory before git in a way tq does not follow; use `cd <dir> && git …` as the first step or `git -C <dir>`"}
	case g.GitDirOverride:
		return "", &DirError{Reason: "the command sets --git-dir, --work-tree, GIT_DIR or GIT_WORK_TREE; tq cannot tell what it records"}
	case g.envChdir:
		return "", &DirError{Reason: "the command runs git through `env -C`; use `git -C <dir>`"}
	}
	d := cwd
	var err error
	if g.Base != "" {
		if d, err = ResolvePath(d, g.Base); err != nil {
			return "", err
		}
	}
	for _, c := range g.Dirs {
		if d, err = ResolvePath(d, c); err != nil {
			return "", err
		}
	}
	if err := existingDir(d); err != nil {
		return "", err
	}
	return d, nil
}

// BaseDir returns the directory a whole command starts in: cwd, or the
// target of a leading `cd <dir>` (first segment only). A later cd, or a
// leading one tq cannot evaluate, is an error.
func BaseDir(cwd, command string) (string, error) {
	masked := maskQuoted(command)
	d := cwd
	seen := false
	for k, sp := range segmentSpans(masked) {
		words := splitWords(command[sp.start:sp.end])
		if len(words) == 0 {
			continue
		}
		dir, ok := cdTarget(words)
		if !ok {
			continue
		}
		if k != 0 || dir == "" || seen {
			return "", &DirError{Reason: "the command changes directory in a way tq does not follow; use `cd <dir> && …` as the first step"}
		}
		r, err := ResolvePath(cwd, dir)
		if err != nil {
			return "", err
		}
		d, seen = r, true
	}
	if seen {
		if err := existingDir(d); err != nil {
			return "", err
		}
	}
	return d, nil
}
