// Package commitmsg checks the message a `git commit` command will record
// against Conventional Commits (type(scope)!: summary; a breaking change
// needs a body or a BREAKING CHANGE footer).
package commitmsg

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
)

// Result of one check. Checked is false when the command carries no message
// tq can read (editor, --amend --no-edit, --fixup, --squash, a message built
// by a shell substitution, no git commit).
type Result struct {
	Checked bool
	OK      bool
	Why     string
}

var (
	headerRe  = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([^()\s]+\))?(!)?: \S.*$`)
	heredocRe = regexp.MustCompile(`(?s)^\$\(\s*cat\s+<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?[^\n]*\n(.*?)\n[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*\n?\s*\)$`)
	maxFile   = 64 << 10
	// commitWordRe spots a git commit anywhere in the raw text, quotes or not.
	commitWordRe = regexp.MustCompile(`(?s)\bgit\b.*\bcommit\b`)
)

// Check inspects command. cwd resolves a relative -F file. It reads at most one
// small file and runs no git.
func Check(command, cwd string) Result {
	cmds, unterminated := commands(command)
	res := Result{OK: true}
	for _, words := range cmds {
		inv, ok := guard.ParseGitWords(words)
		if !ok || inv.Sub != "commit" {
			continue
		}
		r := checkOne(inv.Args, commitDir(command, cwd, inv))
		if r.Checked && !r.OK {
			return r // the first failing commit decides
		}
		if r.Checked {
			res = r
		}
	}
	if unterminated && commitWordRe.MatchString(command) {
		// The open quote swallowed the rest of the command, so a later
		// `git commit` was never parsed.
		return Result{Checked: true, OK: false, Why: "could not parse the command safely; quote the message with matching quotes"}
	}
	return res
}

// commitDir is the directory a commit runs in (a leading `cd <dir>`, then
// every -C), or "" when tq cannot tell (see guard.GitInvocation.Dir).
func commitDir(command, cwd string, inv guard.GitInvocation) string {
	d, err := guard.BaseDir(cwd, command)
	if err != nil || inv.GitDirOverride {
		return ""
	}
	for _, c := range inv.Dirs {
		if d, err = guard.ResolvePath(d, c); err != nil {
			return ""
		}
	}
	if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
		return ""
	}
	return d
}

// checkOne judges the arguments of one git commit. cwd is "" when the
// directory the commit runs in is unknown.
func checkOne(args []string, cwd string) Result {
	var msgs []string
	var file string
	skip := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--amend", a == "--no-edit", strings.HasPrefix(a, "--fixup"), strings.HasPrefix(a, "--squash"), a == "-C", a == "-c", a == "--reuse-message", a == "--reedit-message":
			skip = true
		case a == "--message" || (len(a) > 2 && a[0] == '-' && a[1] != '-' && strings.Trim(a[1:len(a)-1], "aeipsnvq") == "" && a[len(a)-1] == 'm') || a == "-m":
			if i+1 < len(args) {
				msgs = append(msgs, args[i+1])
				i++
			}
		case strings.HasPrefix(a, "--message="):
			msgs = append(msgs, strings.TrimPrefix(a, "--message="))
		case strings.HasPrefix(a, "-m") && !strings.HasPrefix(a, "--"):
			msgs = append(msgs, a[2:])
		case a == "-F" || a == "--file":
			if i+1 < len(args) {
				file = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--file="):
			file = strings.TrimPrefix(a, "--file=")
		}
	}
	if skip && len(msgs) == 0 && file == "" {
		return Result{OK: true}
	}
	var text string
	switch {
	case file != "":
		if file == "-" {
			return Result{OK: true} // stdin: nothing readable here
		}
		if !filepath.IsAbs(file) {
			if cwd == "" {
				return Result{Checked: true, OK: false, Why: "the -F message file is relative and tq cannot tell which directory the commit runs in; pass an absolute path"}
			}
			file = filepath.Join(cwd, file)
		}
		raw, bad := readMessageFile(file)
		if bad != "" {
			return Result{Checked: true, OK: false, Why: bad}
		}
		text = raw
	case len(msgs) > 0:
		for i, m := range msgs {
			if m = unwrapHeredoc(m); strings.HasPrefix(m, "$(") || strings.HasPrefix(m, "`") {
				return Result{OK: true} // built by a substitution tq cannot evaluate
			}
			msgs[i] = m
		}
		text = strings.Join(msgs, "\n\n")
	default:
		return Result{OK: true} // editor: nothing to check here
	}
	return judge(text)
}

// readMessageFile reads a -F message file without ever blocking or reading
// much: only a regular file is opened, and at most maxFile bytes are read.
// It returns a reason when the file cannot be used.
func readMessageFile(path string) (string, string) {
	st, err := os.Stat(path)
	if err != nil {
		return "", "the -F message file could not be read; write it inside the repository first"
	}
	if !st.Mode().IsRegular() {
		return "", "the -F message file is not a regular file; write the message to a plain file first"
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "the -F message file could not be read; write it inside the repository first"
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, int64(maxFile)+1))
	if err != nil {
		return "", "the -F message file could not be read; write it inside the repository first"
	}
	if len(raw) > maxFile {
		return "", "the -F message file is over 64 KiB; keep the message short"
	}
	return string(raw), ""
}

// unwrapHeredoc turns a `$(cat <<'WORD' ... WORD)` message into its body.
func unwrapHeredoc(m string) string {
	if g := heredocRe.FindStringSubmatch(strings.TrimSpace(m)); g != nil && g[1] == g[3] {
		return g[2]
	}
	return m
}

// commands splits a shell command into simple commands of unquoted words.
// Quotes group words and are removed; unquoted ; & | ( ) { } and newlines end
// a command. An unterminated quote runs to the end of the input.
//
// The words of each command are read as a git call by guard.ParseGitWords,
// the same reader every other commit rule uses.
func commands(s string) ([][]string, bool) {
	var out [][]string
	var words []string
	var cur strings.Builder
	in := false
	var q rune
	endWord := func() {
		if in {
			words = append(words, cur.String())
			cur.Reset()
			in = false
		}
	}
	endCmd := func() {
		endWord()
		if len(words) > 0 {
			out = append(out, words)
			words = nil
		}
	}
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
			case r == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\\' || rs[i+1] == '$' || rs[i+1] == '`'):
				i++
				cur.WriteRune(rs[i])
			default:
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			q, in = r, true
		case r == '$' && i+1 < len(rs) && (rs[i+1] == '\'' || rs[i+1] == '"'):
			i++
			q, in = rs[i], true
		case r == '\\' && i+1 < len(rs):
			// the same word rule as guard's reader: a backslash escapes a
			// quote, a space or a backslash and is otherwise kept
			if strings.ContainsRune("\"' \\", rs[i+1]) {
				i++
			}
			cur.WriteRune(rs[i])
			in = true
		case r == ' ' || r == '\t' || r == '\r':
			endWord()
		case r == ';' || r == '&' || r == '|' || r == '\n' || r == '(' || r == ')' || r == '{' || r == '}':
			endCmd()
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	endCmd()
	return out, q != 0
}

func judge(text string) Result {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(strings.TrimSpace(text), "\n")
	header := strings.TrimSpace(lines[0])
	m := headerRe.FindStringSubmatch(header)
	if m == nil {
		return Result{Checked: true, OK: false, Why: "the summary is not Conventional Commits: `type(scope): imperative summary` with type one of feat fix docs style refactor perf test build ci chore revert, lower-case"}
	}
	if len(header) > 100 {
		return Result{Checked: true, OK: false, Why: "the summary line is over 100 characters"}
	}
	breaking := m[3] == "!" || strings.Contains(text, "BREAKING CHANGE:")
	if breaking {
		body := strings.TrimSpace(strings.Join(lines[1:], "\n"))
		if body == "" {
			return Result{Checked: true, OK: false, Why: "a breaking change (`!` or BREAKING CHANGE) needs a body that says what breaks and how to migrate (a MAJOR release note)"}
		}
	}
	return Result{Checked: true, OK: true}
}
