// Package commitmsg checks the message a `git commit` command will record
// against Conventional Commits (type(scope)!: summary; a breaking change
// needs a body or a BREAKING CHANGE footer).
package commitmsg

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
)

// Check inspects command. cwd resolves a relative -F file. It reads at most one
// small file and runs no git.
func Check(command, cwd string) Result {
	for _, words := range commands(command) {
		args, ok := commitArgs(words)
		if !ok {
			continue
		}
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
				file = filepath.Join(cwd, file)
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return Result{Checked: true, OK: false, Why: "the -F message file could not be read; write it inside the repository first"}
			}
			if len(raw) > maxFile {
				raw = raw[:maxFile]
			}
			text = string(raw)
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
	return Result{OK: true}
}

// unwrapHeredoc turns a `$(cat <<'WORD' ... WORD)` message into its body.
func unwrapHeredoc(m string) string {
	if g := heredocRe.FindStringSubmatch(strings.TrimSpace(m)); g != nil && g[1] == g[3] {
		return g[2]
	}
	return m
}

// commitArgs returns the words after `commit` when words is a git commit
// (leading VAR=x assignments and git's global options are skipped).
func commitArgs(words []string) ([]string, bool) {
	i := 0
	for i < len(words) && strings.Contains(words[i], "=") && !strings.HasPrefix(words[i], "-") {
		i++
	}
	if i >= len(words) || words[i] != "git" {
		return nil, false
	}
	for i++; i < len(words); i++ {
		a := words[i]
		switch {
		case a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace":
			i++
		case strings.HasPrefix(a, "-"):
		case a == "commit":
			return words[i+1:], true
		default:
			return nil, false
		}
	}
	return nil, false
}

// commands splits a shell command into simple commands of unquoted words.
// Quotes group words and are removed; unquoted ; & | ( ) { } and newlines end
// a command. An unterminated quote runs to the end of the input.
//
// guard.GitInvocations is not used here: it splits on parentheses, ; and &
// without regard to quotes, which cuts `-m "feat(api): x"` in half.
func commands(s string) [][]string {
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
		case r == '\\' && i+1 < len(rs):
			i++
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
	return out
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
