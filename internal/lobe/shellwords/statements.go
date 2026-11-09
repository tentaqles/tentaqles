// Copied from internal/guard/gitinv.go (splitWords, segmentSpans and their helpers); the lobe may not import the guard.

package shellwords

import (
	"regexp"
	"strings"
	"sync"
)

// sepPattern is the guard's sepPattern (internal/guard/guard.go), the separators segmentSpans splits at.
const sepPattern = `&&|\|\||;|\||&|\n|\r|\$\(|` + "`" + `|\(|\)|\{|\}`

// redirectPattern matches a shell redirection: `2>&1` or `>&2` (a file descriptor copy), `> out.txt`,
// `2>/dev/null`, `>>log` (a file, the blank before it optional), and `&> all.log`. The target of a
// file redirection stops at a separator, so `>x;rm` keeps its `;`.
const redirectPattern = `\d*>&\d+|\d*>>?[ \t]*[^\s;&|()<>` + "`" + `]+|&>[ \t]*[^\s;&|()<>` + "`" + `]+`

// The compiled patterns. They are set by the once functions, never by a package-level initializer.
var (
	sepOnce     sync.Once
	sepRE       *regexp.Regexp
	redirectOne sync.Once
	redirectRE  *regexp.Regexp
)

// StripRedirects returns command with its shell redirections removed (see redirectPattern), so the
// rules that split a command into statements do not read `2>&1` as a separator and a word. The text
// is otherwise unchanged; quotes are not looked at.
func StripRedirects(command string) string {
	redirectOne.Do(func() { redirectRE = regexp.MustCompile(redirectPattern) })
	return redirectRE.ReplaceAllString(command, "")
}

// Statements splits a command line into its statements, exactly as the guard's segmentSpans does:
// at `;`, `&&`, `||`, `|` and newlines that are not inside quotes. Each statement is trimmed of
// surrounding white space; empty statements are dropped.
//
// As in the guard's gitSegmentsDepth, the separators are found in maskQuoted(command), so a
// separator inside quotes does not split; the statement text is taken from command itself.
func Statements(command string) []string {
	sepOnce.Do(func() { sepRE = regexp.MustCompile(sepPattern) })
	sep := sepRE
	masked := maskQuoted(command)
	var out []string
	prev := 0
	add := func(end int) {
		if s := strings.TrimSpace(command[prev:end]); s != "" {
			out = append(out, s)
		}
	}
	for _, loc := range sep.FindAllStringIndex(masked, -1) {
		add(loc[0])
		prev = loc[1]
	}
	add(len(command))
	return out
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
		// A ${...} parameter expansion is one word, not a brace group: blank
		// its braces so segmentSpans does not split `${HOME}/x` at them. The
		// value is expanded later (internal/protect). Single quotes take it
		// literally, so skip there.
		if (q == 0 || q == '"') && ch == '$' && i+1 < len(b) && b[i+1] == '{' {
			maskBraces(b, i+1)
		}
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

// maskBraces blanks the braces of the ${...} expansion whose opening brace
// is at index open (b[open]=='{'), matching nested braces, so sepRegexp does
// not treat them as separators.
func maskBraces(b []byte, open int) {
	depth := 0
	for j := open; j < len(b); j++ {
		switch b[j] {
		case '{':
			depth++
			b[j] = 0x01
		case '}':
			depth--
			b[j] = 0x01
			if depth == 0 {
				return
			}
		}
	}
}

func isSepByte(ch byte) bool {
	switch ch {
	case '&', '|', ';', '\n', '\r', '(', ')', '{', '}', '`', '$':
		return true
	}
	return false
}
