package policy

import (
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
)

// Text masking for the identity category's text scans (ADR-0006 round 2): the parts of a
// shell line that are text, not expansion or code, must not match a provider. These are
// blanked to spaces before the env/host/mention scans run:
//   - grep/rg/findstr/Select-String search patterns,
//   - git commit messages (-m/-am/--message),
//   - PowerShell here-strings (@'...'@ and @"..."@),
//   - and, for the bash expansion/assignment scan only, single-quoted spans (no expansion).
// The explicit env APIs (process.env, [Environment]::..., os.environ[...]) run on the
// grep/commit/here-string-blanked text but NOT the single-quote-blanked one, because a real
// env write such as [Environment]::SetEnvironmentVariable('AWS_PROFILE',...) uses single
// quotes as string delimiters, not as bash no-expansion quoting.

var hereStringRe = regexp.MustCompile(`(?s)@'.*?'@|@".*?"@`)

// maskTextArgs blanks here-strings, grep/findstr/Select-String patterns and git commit
// messages in command (which already has bash data heredocs blanked by the caller).
func maskTextArgs(command string) string {
	b := []byte(command)
	blankSpan := func(sub string) {
		sub = strings.TrimSpace(sub)
		if len(sub) < 2 {
			return
		}
		if i := strings.Index(command, sub); i >= 0 {
			for j := i; j < i+len(sub) && j < len(b); j++ {
				if b[j] != '\n' {
					b[j] = ' '
				}
			}
		}
	}
	for _, c := range guard.Commands(command) {
		verb, args, ok := guard.Verb(c.Words)
		if !ok {
			continue
		}
		name := exeName(verb)
		switch {
		case grepLike[name]:
			for _, a := range args {
				if !strings.HasPrefix(a, "-") {
					blankSpan(a)
					break
				}
			}
		case name == "findstr":
			for _, a := range args {
				if !strings.HasPrefix(a, "/") && !strings.HasPrefix(a, "-") {
					blankSpan(a)
					break
				}
			}
		case name == "select-string" || name == "sls":
			for i, a := range args {
				if (strings.EqualFold(a, "-Pattern") || strings.EqualFold(a, "-SimpleMatch")) && i+1 < len(args) {
					blankSpan(args[i+1])
					break
				}
				if !strings.HasPrefix(a, "-") {
					blankSpan(a)
					break
				}
			}
		case name == "git":
			commit := false
			for i, a := range args {
				if a == "commit" {
					commit = true
				}
				if !commit {
					continue
				}
				if (a == "-m" || a == "--message" || a == "-am" || a == "-c" || a == "--reedit-message" || a == "-C") && i+1 < len(args) {
					blankSpan(args[i+1])
				}
				if v, ok := strings.CutPrefix(a, "--message="); ok {
					blankSpan(v)
				}
				if v, ok := strings.CutPrefix(a, "-m"); ok && v != "" {
					blankSpan(v)
				}
			}
		}
	}
	out := string(b)
	return hereStringRe.ReplaceAllStringFunc(out, func(m string) string {
		return strings.Repeat(" ", len(m))
	})
}

// grepLike are commands whose first non-option argument is a search pattern, not code.
var grepLike = wordSet("grep", "egrep", "fgrep", "rg", "ag", "ack", "ripgrep")

// blankSingleQuoted blanks bash/PowerShell single-quoted spans (literal, no expansion),
// honouring double-quote context so a ' inside "..." is not treated as a quote.
func blankSingleQuoted(command string) string {
	b := []byte(command)
	const none, single, double = 0, 1, 2
	state := none
	for i := 0; i < len(b); i++ {
		ch := b[i]
		switch state {
		case none:
			switch ch {
			case '\'':
				state = single
				b[i] = ' '
			case '"':
				state = double
			case '\\':
				i++
			}
		case single:
			if ch == '\'' {
				state = none
				b[i] = ' '
			} else if ch != '\n' {
				b[i] = ' '
			}
		case double:
			if ch == '"' {
				state = none
			} else if ch == '\\' {
				i++
			}
		}
	}
	return string(b)
}
