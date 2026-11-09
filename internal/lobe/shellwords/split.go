// Copied from internal/guard/gitinv.go (splitWords, segmentSpans and their helpers); the lobe may not import the guard.

package shellwords

import "strings"

// Split splits one simple command into words, exactly as the guard's splitWords does: '…' and $'…'
// are literal; "…" and $"…" honor \" \\ \$ and a backtick; a backslash outside quotes escapes a quote, a space or
// a backslash and is otherwise kept; an open quote runs to the end.
func Split(command string) []string {
	var out []string
	var cur strings.Builder
	in := false
	var q rune
	rs := []rune(command)
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
		case r == '$' && i+1 < len(rs) && (rs[i+1] == '\'' || rs[i+1] == '"'):
			i++
			q, in = rs[i], true
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

// Program is the program word of a simple command: the first word that is not an environment
// assignment (NAME=value, where NAME is letters, digits and underscores and does not start with a digit),
// without its folder (anything up to the last `/` or `\`), without a trailing `.exe` (case ignored), and
// lowercased (Windows program names are case-insensitive). It is "" when the command has no such word.
func Program(command string) string {
	for _, w := range Split(command) {
		if IsAssignment(w) {
			continue
		}
		base := w
		if i := strings.LastIndexAny(base, `/\`); i >= 0 {
			base = base[i+1:]
		}
		if len(base) >= 4 && strings.EqualFold(base[len(base)-4:], ".exe") {
			base = base[:len(base)-4]
		}
		return strings.ToLower(base)
	}
	return ""
}

// CommandWords is the words of one simple command from its program word on: the NAME=value
// assignments before the program are dropped. It is nil when the statement has no words.
func CommandWords(statement string) []string {
	words := Split(statement)
	for len(words) > 0 && IsAssignment(words[0]) {
		words = words[1:]
	}
	return words
}

// IsAssignment reports whether w is NAME=value with NAME made of letters, digits and underscores, not
// starting with a digit.
func IsAssignment(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	name := w[:eq]
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
