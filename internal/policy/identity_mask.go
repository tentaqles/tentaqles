package policy

import (
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
)

// Text masking for the identity category's text scans (ADR-0007 rounds 2 and 3): the parts of
// a shell line that are text, not expansion or code, must not match a provider. These are
// blanked to spaces before the env/host/mention scans run:
//   - grep/rg/findstr/Select-String search patterns,
//   - git commit messages (-m/-am/--message),
//   - PowerShell here-strings (@'...'@ always; @"..."@ only without $ or a backtick),
//   - and, for the bash expansion/assignment scan only, single-quoted spans (no expansion).
//
// Each shell is read with its own quoting: bash escapes with a backslash (outside quotes and
// inside double quotes); PowerShell escapes with a backtick and doubles a quote inside a
// string ('it''s', "say ""hi"""), and a backslash is an ordinary character. The Bash tool's
// line is read as bash, the PowerShell tool's and a decoded -EncodedCommand's as PowerShell,
// and the string of a nested `pwsh -Command "..."` or `bash -c '...'` with the inner shell's
// rules.
//
// Only literal text is ever blanked, and the masker fails closed:
//   - a word is blanked by the byte span the masker's own scanner recorded for it (never by
//     searching for its text, which could hit an identical earlier substring);
//   - never when it holds an expansion ($, or a backtick: a bash substitution, a PowerShell
//     escape), so an expansion inside a commit message or a grep pattern is still scanned;
//   - nothing at all when a quote, a here-string or a $( is left unterminated;
//   - never a span that crosses a separator the guard tokenizer saw (guard.SeparatorSpans):
//     where the two readers disagree about what is quoted, the span is scanned.
//
// The explicit env APIs (process.env, [Environment]::..., os.environ[...]) run on the masked
// text but NOT the single-quote-blanked one, because a real env write such as
// [Environment]::SetEnvironmentVariable('AWS_PROFILE',...) uses single quotes as string
// delimiters, not as bash no-expansion quoting.

// maskWord is one word of a shell line with its byte span in the line.
type maskWord struct {
	start, end int    // [start, end) in the line
	text       string // quotes removed
	expands    bool   // holds $ or a backtick outside single quotes
	quoted     bool   // the whole word is one quoted string with no escapes: text is s[start+1:end-1]
	here       bool   // a PowerShell here-string
}

// maskSegments splits a line into simple commands of words, keeping each word's byte span,
// with bash rules or (ps) PowerShell rules. Separators (; & | newline) split only outside
// quotes and outside $( ... ). ok is false when a quote, a here-string or a $( is left open:
// the caller then blanks nothing.
func maskSegments(s string, ps bool) (segs [][]maskWord, ok bool) {
	var cur []maskWord
	var w *maskWord
	var txt strings.Builder
	const none, single, double = 0, 1, 2
	state, depth := none, 0
	escaped := false // the current word holds an escape (its text differs from its span)
	quotes := 0      // quoted strings in the current word
	flush := func(end int) {
		if w != nil {
			w.end = end
			w.text = txt.String()
			raw := s[w.start:end]
			w.quoted = quotes == 1 && !escaped && len(raw) >= 2 && (raw[0] == '\'' || raw[0] == '"') &&
				raw[len(raw)-1] == raw[0] && raw[1:len(raw)-1] == w.text
			cur = append(cur, *w)
			w = nil
			txt.Reset()
			escaped, quotes = false, 0
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
				if ps && i+1 < len(s) && s[i+1] == '\'' { // PowerShell: '' is a quote
					escaped = true
					txt.WriteByte('\'')
					i++
					continue
				}
				state = none
			} else {
				txt.WriteByte(ch)
			}
			continue
		case double:
			switch {
			case ch == '"' && ps && i+1 < len(s) && s[i+1] == '"': // PowerShell: "" is a quote
				escaped = true
				txt.WriteByte('"')
				i++
			case ch == '"':
				state = none
			case ch == '\\' && !ps:
				escaped = true
				if i+1 < len(s) {
					i++
					txt.WriteByte(s[i])
				}
			case ch == '`' && ps: // PowerShell's escape character
				w.expands = true
				if i+1 < len(s) {
					txt.WriteByte(ch)
					i++
					txt.WriteByte(s[i])
				}
			case ch == '$' || ch == '`':
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
		// A PowerShell here-string: @' or @" ending its line, closed by '@ or "@ at the
		// start of a line. It is one word.
		if ps && w == nil && depth == 0 && ch == '@' && i+1 < len(s) && (s[i+1] == '\'' || s[i+1] == '"') {
			if end, body, ok := hereString(s, i); ok {
				hw := maskWord{start: i, end: end, text: body, here: true}
				hw.expands = s[i+1] == '"' && strings.ContainsAny(body, "$`")
				cur = append(cur, hw)
				i = end - 1
				continue
			} else if end < 0 {
				return nil, false // unterminated
			}
		}
		begin(i)
		switch ch {
		case '\'':
			state = single
			quotes++
		case '"':
			state = double
			quotes++
		case '\\':
			if ps {
				txt.WriteByte(ch) // an ordinary character in PowerShell
				break
			}
			escaped = true
			if i+1 < len(s) {
				i++
				txt.WriteByte(s[i])
			}
		case '`':
			w.expands = true
			if ps { // PowerShell's escape character
				escaped = true
				if i+1 < len(s) {
					i++
					txt.WriteByte(s[i])
				}
				break
			}
			txt.WriteByte(ch)
		case '$':
			w.expands = true
			if i+1 < len(s) && s[i+1] == '(' {
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
	if state != none || depth != 0 {
		return nil, false
	}
	flush(len(s))
	if len(cur) > 0 {
		segs = append(segs, cur)
	}
	return segs, true
}

// hereString reads a PowerShell here-string opening at s[i] ('@'): ok with its end (past the
// closing '@ or "@) and its body; end 0 when s[i:] is not a here-string opener (the rest of
// the line after @' is not blank); end -1 when it is one but never closed.
func hereString(s string, i int) (end int, body string, ok bool) {
	q := s[i+1]
	j := i + 2
	for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r') {
		j++
	}
	if j >= len(s) || s[j] != '\n' {
		return 0, "", false
	}
	closer := "\n" + string(q) + "@"
	k := strings.Index(s[j:], closer)
	if k < 0 {
		return -1, "", false
	}
	return j + k + len(closer), s[j+1 : j+k], true
}

// maskWrappers precede the verb of a simple command.
var maskWrappers = wordSet("sudo", "env", "command", "builtin", "exec", "time", "nohup", "nice", "&")

var maskAssignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// maskTextArgsFor is maskTextArgs with the shell named: "powershell" (or "pwsh") for
// PowerShell's rules, anything else for bash's.
func maskTextArgsFor(command, shell string) string {
	s := strings.ToLower(shell)
	return maskTextArgs(command, s == "powershell" || s == "pwsh")
}

// maskTextArgs blanks here-strings, grep/findstr/Select-String patterns and git commit
// messages in command (which already has bash data heredocs blanked by the caller), by the
// spans of the literal words only, with PowerShell's quoting rules when ps is set.
func maskTextArgs(command string, ps bool) string {
	b := []byte(command)
	maskInto(b, command, ps, 0)
	return string(b)
}

// maskInto blanks the text words of command into b (the same length), b[i] for command[i].
func maskInto(b []byte, command string, ps bool, depth int) {
	segs, ok := maskSegments(command, ps)
	if !ok {
		return // an unterminated quote: blank nothing
	}
	seps := guard.SeparatorSpans(command)
	crosses := func(start, end int) bool {
		for _, sp := range seps {
			if sp[0] < end && sp[1] > start {
				return true
			}
		}
		return false
	}
	blank := func(w maskWord, from int) {
		if w.expands {
			return // an expansion or substitution is scanned, never treated as text
		}
		start := w.start + from
		if crosses(start, w.end) {
			return // the tokenizer sees a command boundary inside: not one word to it
		}
		for j := start; j < w.end && j < len(b); j++ {
			if b[j] != '\n' {
				b[j] = ' '
			}
		}
	}
	for _, seg := range segs {
		// here-strings are text wherever they stand
		for _, w := range seg {
			if w.here {
				blank(w, 0)
			}
		}
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
		case depth < 2 && (name == "pwsh" || name == "powershell" || name == "bash" || name == "sh" || name == "zsh"):
			// the string of a nested shell is masked with that shell's rules, in place
			innerPS := name == "pwsh" || name == "powershell"
			for k, a := range args {
				if !nestedCommandFlag(a.text, innerPS) || k+1 >= len(args) {
					continue
				}
				in := args[k+1]
				if in.quoted && !crosses(in.start, in.end) {
					maskInto(b[in.start+1:in.end-1], in.text, innerPS, depth+1)
				}
				break
			}
		}
	}
}

// nestedCommandFlag reports whether a is the option that makes the next word a command
// string: -c for bash/sh/zsh; -c, -Command or an unambiguous prefix of it for PowerShell.
func nestedCommandFlag(a string, ps bool) bool {
	if !ps {
		return a == "-c" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.HasSuffix(a, "c") && len(a) <= 4)
	}
	l := strings.ToLower(a)
	return len(l) >= 2 && strings.HasPrefix("-command", l)
}

// grepLike are commands whose first non-option argument is a search pattern, not code.
var grepLike = wordSet("grep", "egrep", "fgrep", "rg", "ag", "ack", "ripgrep")

// blankSingleQuoted blanks single-quoted spans (literal, no expansion) for the bash
// expansion/assignment scan, honouring double-quote context so a ' inside "..." is not a
// quote, with PowerShell's rules when ps is set (a doubled quote inside a single-quoted string, a backtick
// escape, a backslash ordinary). Like the masker it fails closed: nothing is blanked when a
// quote is left open, and a span that crosses a separator the guard tokenizer saw is kept.
func blankSingleQuoted(command string, ps bool) string {
	b := []byte(command)
	const none, single, double = 0, 1, 2
	state := none
	var spans [][2]int
	open := 0
	for i := 0; i < len(b); i++ {
		ch := b[i]
		switch state {
		case none:
			switch {
			case ch == '\'':
				state, open = single, i
			case ch == '"':
				state = double
			case ch == '\\' && !ps, ch == '`' && ps:
				i++
			}
		case single:
			if ch == '\'' {
				if ps && i+1 < len(b) && b[i+1] == '\'' {
					i++
					continue
				}
				state = none
				spans = append(spans, [2]int{open, i + 1})
			}
		case double:
			switch {
			case ch == '"':
				state = none
			case ch == '\\' && !ps, ch == '`' && ps:
				i++
			}
		}
	}
	if state != none {
		return command
	}
	seps := guard.SeparatorSpans(command)
	for _, sp := range spans {
		cross := false
		for _, sep := range seps {
			if sep[0] < sp[1] && sep[1] > sp[0] {
				cross = true
				break
			}
		}
		if cross {
			continue
		}
		for j := sp[0]; j < sp[1]; j++ {
			if b[j] != '\n' {
				b[j] = ' '
			}
		}
	}
	return string(b)
}
