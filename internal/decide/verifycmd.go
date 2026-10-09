package decide

import (
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
)

// IsVerificationCommand reports whether a shell command line (bash or
// PowerShell) runs a test, build, lint or type-check tool. It is the
// deterministic gate for completion evidence, so it is deliberately
// narrow: a segment counts only when its first command word is a known
// runner. A runner name that merely appears somewhere in the command (in
// an echo, a grep pattern, a quoted string, a comment or a heredoc body)
// never counts, and a runner whose failure is masked with `||` does not
// either.
//
// This is a heuristic, not a shell parser. On anything it cannot read
// (an unterminated quote) it returns false: missing evidence only makes
// the advisory check stricter.
func IsVerificationCommand(cmd string) bool {
	segs, ok := shellSegments(guard.StripHeredocs(cmd))
	if !ok {
		return false
	}
	for _, s := range segs {
		if s.next == "||" {
			continue // `npm test || true` hides a failure
		}
		if isRunnerSegment(s.words) {
			return true
		}
	}
	return false
}

type shWord struct {
	text   string
	quoted bool // any part of the word was quoted or escaped
}

type shSeg struct {
	words []shWord
	next  string // the separator after this segment
}

// shellSegments splits a command on unquoted separators (&& || ; | & and
// newlines, plus the grouping and substitution openers ( ) { } $( and the
// backtick), drops unquoted # comments, and splits each segment into
// words. Quoted text stays inside its word and marks it quoted. ok is
// false on an unterminated quote.
func shellSegments(cmd string) ([]shSeg, bool) {
	var (
		segs   []shSeg
		cur    shSeg
		w      strings.Builder
		inWord bool
		quoted bool
	)
	flushWord := func() {
		if inWord {
			cur.words = append(cur.words, shWord{text: w.String(), quoted: quoted})
		}
		w.Reset()
		inWord, quoted = false, false
	}
	flushSeg := func(sep string) {
		flushWord()
		cur.next = sep
		if len(cur.words) > 0 {
			segs = append(segs, cur)
		}
		cur = shSeg{}
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		next := byte(0)
		if i+1 < len(cmd) {
			next = cmd[i+1]
		}
		switch {
		case c == '\'':
			end := strings.IndexByte(cmd[i+1:], '\'')
			if end < 0 {
				return nil, false
			}
			w.WriteString(cmd[i+1 : i+1+end])
			inWord, quoted = true, true
			i += end + 1
		case c == '"':
			j := i + 1
			for ; j < len(cmd) && cmd[j] != '"'; j++ {
				if cmd[j] == '\\' {
					j++
				}
			}
			if j >= len(cmd) {
				return nil, false
			}
			w.WriteString(cmd[i+1 : j])
			inWord, quoted = true, true
			i = j
		case c == '\\':
			if next != 0 {
				w.WriteByte(next)
				i++
			}
			inWord, quoted = true, true
		case c == '#' && !inWord:
			for i+1 < len(cmd) && cmd[i+1] != '\n' && cmd[i+1] != '\r' {
				i++
			}
		case c == ' ' || c == '\t':
			flushWord()
		case c == '\n' || c == '\r':
			flushSeg("\n")
		case c == '&':
			// 2>&1, &>file and >&2 are redirections, not separators.
			if (inWord && strings.HasSuffix(w.String(), ">")) || next == '>' {
				w.WriteByte(c)
				inWord = true
			} else if next == '&' {
				flushSeg("&&")
				i++
			} else {
				flushSeg("&")
			}
		case c == '|':
			if next == '|' {
				flushSeg("||")
				i++
			} else {
				flushSeg("|")
			}
		case c == ';' || c == '(' || c == ')' || c == '{' || c == '}' || c == '`':
			flushSeg(string(c))
		case c == '$' && next == '(':
			flushSeg("$(")
			i++
		default:
			w.WriteByte(c)
			inWord = true
		}
	}
	flushSeg("")
	return segs, true
}

var envAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// standalone runners: the tool name alone is a check.
var standaloneRunners = map[string]bool{
	"pytest": true, "py.test": true, "tsc": true, "eslint": true, "ruff": true, "mypy": true,
	"pyright": true, "flake8": true, "golangci-lint": true, "staticcheck": true, "shellcheck": true,
	"jest": true, "vitest": true, "mocha": true, "tox": true, "nox": true, "rspec": true,
	"phpunit": true, "ctest": true, "invoke-pester": true, "biome": true,
}

// subcommand runners: the tool plus one of these first arguments.
var subRunners = map[string]map[string]bool{
	"go":      {"test": true, "vet": true, "build": true},
	"cargo":   {"test": true, "clippy": true, "build": true, "check": true},
	"make":    {"test": true, "check": true, "lint": true},
	"dotnet":  {"test": true, "build": true},
	"mvn":     {"test": true, "verify": true},
	"gradle":  {"test": true, "check": true, "build": true},
	"gradlew": {"test": true, "check": true, "build": true},
}

var scriptNames = map[string]bool{"test": true, "lint": true, "build": true, "typecheck": true, "check": true}

func scriptName(s string) bool {
	if scriptNames[s] {
		return true
	}
	head, _, found := strings.Cut(s, ":") // test:unit, lint:ci
	return found && scriptNames[head]
}

// isRunnerSegment checks the first command word of one segment, after
// VAR=val assignments and env/time/npx/uv run/poetry run prefixes.
func isRunnerSegment(ws []shWord) bool {
	for len(ws) > 0 {
		w := ws[0]
		if w.quoted {
			return false // a quoted command word is never trusted
		}
		lw := strings.ToLower(w.text)
		switch {
		case envAssign.MatchString(w.text):
			ws = ws[1:]
		case lw == "env" || lw == "time" || lw == "npx" || lw == "command":
			ws = ws[1:]
		case (lw == "uv" || lw == "poetry" || lw == "pdm") && len(ws) > 1 && strings.ToLower(ws[1].text) == "run":
			ws = ws[2:]
		default:
			return runnerWords(ws)
		}
	}
	return false
}

func runnerWords(ws []shWord) bool {
	name := strings.ToLower(path.Base(strings.ReplaceAll(ws[0].text, `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		name = strings.TrimSuffix(name, ext)
	}
	arg := func(i int) string {
		if i < len(ws) {
			return strings.ToLower(ws[i].text)
		}
		return ""
	}
	switch {
	case standaloneRunners[name]:
		return true
	case subRunners[name] != nil:
		return subRunners[name][arg(1)]
	case name == "python" || name == "python3" || name == "py":
		for i := 1; i+1 < len(ws); i++ {
			if arg(i) == "-m" {
				return arg(i+1) == "pytest" || arg(i+1) == "unittest" || arg(i+1) == "mypy" || arg(i+1) == "ruff"
			}
		}
	case name == "npm" || name == "pnpm" || name == "yarn" || name == "bun":
		a := arg(1)
		if a == "test" || a == "t" {
			return true
		}
		if a == "run" || a == "run-script" {
			return scriptName(arg(2))
		}
		return name != "npm" && scriptName(a) // pnpm lint, yarn build
	case name == "playwright":
		return arg(1) == "test"
	}
	return false
}
