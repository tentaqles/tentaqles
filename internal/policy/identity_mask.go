package policy

import (
	"regexp"
	"strings"
)

// Text masking for the identity category's text scans (ADR-0006 round 2): the parts of a
// shell line that are text, not expansion or code, must not match a provider. These are
// blanked to spaces before the env/host/mention scans run:
//   - grep/rg/findstr/Select-String search patterns,
//   - git commit messages (-m/-am/--message),
//   - PowerShell here-strings (@'...'@ always; @"..."@ only without $ or a backtick),
//   - and, for the bash expansion/assignment scan only, single-quoted spans (no expansion).
//
// Only literal text is ever blanked: a word is blanked by the byte offsets the word scanner
// recorded for it (never by searching for its text, which could hit an identical earlier
// substring), and never when it holds a $ or a backtick outside single quotes, so an
// expansion or a command substitution inside a commit message or a grep pattern is still
// scanned. The explicit env APIs (process.env, [Environment]::..., os.environ[...]) run on
// this text but NOT the single-quote-blanked one, because a real env write such as
// [Environment]::SetEnvironmentVariable('AWS_PROFILE',...) uses single quotes as string
// delimiters, not as bash no-expansion quoting.

var hereStringRe = regexp.MustCompile(`(?s)@'.*?'@|@".*?"@`)

// maskWord is one word of a shell line with its byte span in the line.
type maskWord struct {
	start, end int    // [start, end) in the line
	text       string // quotes removed
	expands    bool   // holds $ or a backtick outside single quotes
}

// maskSegments splits a line into simple commands of words, keeping each word's byte span.
// Separators (; & | newline) split only outside quotes and outside $( ... ).
func maskSegments(s string) [][]maskWord {
	var segs [][]maskWord
	var cur []maskWord
	var w *maskWord
	var txt strings.Builder
	const none, single, double = 0, 1, 2
	state, depth := none, 0
	flush := func(end int) {
		if w != nil {
			w.end = end
			w.text = txt.String()
			cur = append(cur, *w)
			w = nil
			txt.Reset()
		}
	}
	begin := func(i int) {
		if w == nil {
			w = &maskWord{start: i}
		}
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch state {
		case single:
			if ch == '\'' {
				state = none
			} else {
				txt.WriteByte(ch)
			}
			continue
		case double:
			switch ch {
			case '"':
				state = none
			case '\\':
				if i+1 < len(s) {
					i++
					txt.WriteByte(s[i])
				}
			case '$', '`':
				w.expands = true
				txt.WriteByte(ch)
			default:
				txt.WriteByte(ch)
			}
			continue
		}
		// state none
		if depth == 0 && (ch == ' ' || ch == '\t' || ch == '\r') {
			flush(i)
			continue
		}
		if depth == 0 && (ch == ';' || ch == '&' || ch == '|' || ch == '\n') {
			flush(i)
			if len(cur) > 0 {
				segs = append(segs, cur)
				cur = nil
			}
			continue
		}
		begin(i)
		switch ch {
		case '\'':
			state = single
		case '"':
			state = double
		case '\\':
			if i+1 < len(s) {
				i++
				txt.WriteByte(s[i])
			}
		case '$', '`':
			w.expands = true
			if ch == '$' && i+1 < len(s) && s[i+1] == '(' {
				depth++
				i++
				txt.WriteString("$(")
				continue
			}
			txt.WriteByte(ch)
		case '(':
			if depth > 0 {
				depth++
			}
			txt.WriteByte(ch)
		case ')':
			if depth > 0 {
				depth--
			}
			txt.WriteByte(ch)
		default:
			txt.WriteByte(ch)
		}
	}
	flush(len(s))
	if len(cur) > 0 {
		segs = append(segs, cur)
	}
	return segs
}

// maskWrappers precede the verb of a simple command.
var maskWrappers = wordSet("sudo", "env", "command", "builtin", "exec", "time", "nohup", "nice", "&")

var maskAssignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// maskTextArgs blanks here-strings, grep/findstr/Select-String patterns and git commit
// messages in command (which already has bash data heredocs blanked by the caller), by the
// spans of the literal words only.
func maskTextArgs(command string) string {
	b := []byte(command)
	blank := func(w maskWord, from int) {
		if w.expands {
			return // an expansion or substitution is scanned, never treated as text
		}
		for j := w.start + from; j < w.end && j < len(b); j++ {
			if b[j] != '\n' {
				b[j] = ' '
			}
		}
	}
	for _, seg := range maskSegments(command) {
		i := 0
		for i < len(seg) && (maskAssignRe.MatchString(seg[i].text) || maskWrappers[strings.ToLower(seg[i].text)]) {
			i++
		}
		if i >= len(seg) {
			continue
		}
		name := exeName(seg[i].text)
		args := seg[i+1:]
		switch {
		case grepLike[name]:
			for _, a := range args {
				if !strings.HasPrefix(a.text, "-") {
					blank(a, 0)
					break
				}
			}
		case name == "findstr":
			for _, a := range args {
				if !strings.HasPrefix(a.text, "/") && !strings.HasPrefix(a.text, "-") {
					blank(a, 0)
					break
				}
			}
		case name == "select-string" || name == "sls":
			for k, a := range args {
				if (strings.EqualFold(a.text, "-Pattern") || strings.EqualFold(a.text, "-SimpleMatch")) && k+1 < len(args) {
					blank(args[k+1], 0)
					break
				}
				if !strings.HasPrefix(a.text, "-") {
					blank(a, 0)
					break
				}
			}
		case name == "git":
			commit := false
			for k, a := range args {
				if a.text == "commit" {
					commit = true
				}
				if !commit {
					continue
				}
				switch {
				case (a.text == "-m" || a.text == "--message" || a.text == "-am") && k+1 < len(args):
					blank(args[k+1], 0)
				case strings.HasPrefix(a.text, "--message=") && command[a.start:a.start+len("--message=")] == "--message=":
					blank(a, len("--message="))
				case strings.HasPrefix(a.text, "-m") && len(a.text) > 2 && command[a.start:a.start+2] == "-m":
					blank(a, 2)
				}
			}
		}
	}
	out := string(b)
	return hereStringRe.ReplaceAllStringFunc(out, func(m string) string {
		if strings.HasPrefix(m, `@"`) && strings.ContainsAny(m, "$`") {
			return m // an expandable here-string with an expansion is code
		}
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
