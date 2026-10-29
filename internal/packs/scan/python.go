// Package scan holds the shared scanners of the pack framework (spec C
// §2.4): a Python line tokenizer with call sites, notebook cell extraction
// (.ipynb, Fabric notebook-content.py, Synapse notebook JSON), a SQL
// tokenizer with statements and relations, and YAML helpers that keep line
// numbers. Pure Go over the standard library and gopkg.in/yaml.v3.
package scan

import (
	"regexp"
	"strings"
)

// PyLine is one logical Python line: physical lines joined across open
// brackets and backslash continuations, with string literal contents and
// comments removed. String literals stay as empty quotes ("" or ”), and
// their contents are kept in Strings, in order.
type PyLine struct {
	Line, EndLine int // 1-based physical lines
	Indent        int // leading columns of the first physical line (tab = 8)
	Code          string
	Strings       []string
	// Comment holds the comment text of the line's physical lines (for
	// inline suppressions such as `# tq:allow ml/x: reason`).
	Comment string
}

// TopLevel reports whether the line is at module level.
func (l PyLine) TopLevel() bool { return l.Indent == 0 }

// PythonLines tokenizes Python source into logical lines.
func PythonLines(src []byte) []PyLine {
	s := string(src)
	var out []PyLine
	var code, comment strings.Builder
	var strs []string
	line := 1
	cur := PyLine{Line: 1, Indent: -1}
	depth := 0
	flush := func() {
		c := strings.TrimSpace(code.String())
		if c != "" || comment.Len() > 0 {
			cur.Code = collapseSpaces(c)
			cur.Strings = strs
			cur.Comment = strings.TrimSpace(comment.String())
			cur.EndLine = line
			if cur.Indent < 0 {
				cur.Indent = 0
			}
			if c != "" || cur.Comment != "" {
				out = append(out, cur)
			}
		}
		code.Reset()
		comment.Reset()
		strs = nil
		cur = PyLine{Line: line + 1, Indent: -1}
		depth = 0
	}
	atLineStart := true
	col := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if atLineStart {
			if ch == ' ' {
				col++
				continue
			}
			if ch == '\t' {
				col += 8 - col%8
				continue
			}
			if ch == '\r' {
				continue
			}
			if ch != '\n' && cur.Indent < 0 && code.Len() == 0 {
				cur.Indent = col
				cur.Line = line
			}
			atLineStart = false
		}
		switch {
		case ch == '#':
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				j = len(s) - i
			}
			if comment.Len() > 0 {
				comment.WriteByte(' ')
			}
			comment.WriteString(strings.TrimSpace(strings.TrimPrefix(s[i:i+j], "#")))
			i += j - 1
		case ch == '\\' && i+1 < len(s) && (s[i+1] == '\n' || s[i+1] == '\r'):
			// explicit continuation
			if s[i+1] == '\r' && i+2 < len(s) && s[i+2] == '\n' {
				i++
			}
			i++
			line++
			code.WriteByte(' ')
		case ch == '\n':
			if depth > 0 {
				code.WriteByte(' ')
				line++
				continue
			}
			flush()
			line++
			atLineStart = true
			col = 0
		case ch == '(' || ch == '[' || ch == '{':
			depth++
			code.WriteByte(ch)
		case ch == ')' || ch == ']' || ch == '}':
			if depth > 0 {
				depth--
			}
			code.WriteByte(ch)
		case ch == '"' || ch == '\'':
			q := string(ch)
			triple := strings.HasPrefix(s[i:], q+q+q)
			end := q
			start := i + 1
			if triple {
				end = q + q + q
				start = i + 3
			}
			raw := isRawPrefix(code.String())
			j := start
			for j < len(s) {
				if !raw && s[j] == '\\' {
					j += 2
					continue
				}
				if strings.HasPrefix(s[j:], end) {
					break
				}
				if !triple && s[j] == '\n' {
					break
				}
				j++
			}
			if j > len(s) {
				j = len(s)
			}
			body := s[start:j]
			line += strings.Count(body, "\n")
			strs = append(strs, body)
			code.WriteString(q + q)
			switch {
			case j >= len(s):
				i = len(s)
			case !triple && s[j] == '\n':
				i = j - 1 // unterminated: the newline still ends the line
			default:
				i = j + len(end) - 1
			}
		default:
			code.WriteByte(ch)
		}
	}
	flush()
	return out
}

// isRawPrefix reports whether the code before a quote ends in a raw-string
// prefix (r, rb, br, rf, fr in any case).
func isRawPrefix(before string) bool {
	n := len(before)
	k := 0
	for k < 2 && n-k-1 >= 0 && strings.ContainsRune("rRbBfFuU", rune(before[n-k-1])) {
		k++
	}
	if k == 0 {
		return false
	}
	if n-k-1 >= 0 && isIdent(before[n-k-1]) {
		return false // part of a longer name
	}
	return strings.ContainsAny(before[n-k:], "rR")
}

func isIdent(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

var spaceRe = regexp.MustCompile(`\s+`)

func collapseSpaces(s string) string { return spaceRe.ReplaceAllString(s, " ") }

// Call is one call site.
type Call struct {
	Line     int
	TopLevel bool
	// Func is the dotted callee as written ("pd.read_csv", "train_test_split").
	// A call on the result of another call has Func ".name".
	Func     string
	Receiver string // everything before the last dot ("pd"), "" for a bare name
	Name     string // the last segment ("read_csv")
	Args     []string
	Kwargs   map[string]string
	// Strings are the contents of the string literals of the logical line.
	Strings []string
}

var calleeRe = regexp.MustCompile(`([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*\(`)

var pyKeywords = map[string]bool{"if": true, "elif": true, "while": true, "for": true, "return": true, "and": true, "or": true,
	"not": true, "in": true, "is": true, "with": true, "assert": true, "yield": true, "del": true, "print": false, "lambda": true,
	"def": true, "class": true, "except": true, "await": true, "import": true, "from": true, "as": true, "raise": true}

// PythonCalls returns every call site of src.
func PythonCalls(src []byte) []Call {
	var out []Call
	for _, l := range PythonLines(src) {
		out = append(out, LineCalls(l)...)
	}
	return out
}

// LineCalls returns the call sites in one logical line.
func LineCalls(l PyLine) []Call {
	var out []Call
	code := l.Code
	if strings.HasPrefix(code, "def ") || strings.HasPrefix(code, "class ") || strings.HasPrefix(code, "async def ") {
		// The parameter list is not a call; calls in default values are rare.
		return nil
	}
	for _, m := range calleeRe.FindAllStringSubmatchIndex(code, -1) {
		callee := strings.ReplaceAll(code[m[2]:m[3]], " ", "")
		if pyKeywords[callee] {
			continue
		}
		fn := callee
		if m[2] > 0 && code[m[2]-1] == '.' {
			fn = "." + callee
		}
		open := m[1] - 1
		args, kwargs := splitArgs(code, open)
		c := Call{Line: l.Line, TopLevel: l.TopLevel(), Func: fn, Args: args, Kwargs: kwargs, Strings: l.Strings}
		if i := strings.LastIndex(fn, "."); i >= 0 {
			c.Receiver, c.Name = fn[:i], fn[i+1:]
		} else {
			c.Name = fn
		}
		out = append(out, c)
	}
	return out
}

// splitArgs splits the argument list starting at code[open] == '('.
func splitArgs(code string, open int) ([]string, map[string]string) {
	depth := 0
	start := open + 1
	var parts []string
	for i := open; i < len(code); i++ {
		switch code[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				parts = append(parts, code[start:i])
				i = len(code)
			}
		case ',':
			if depth == 1 {
				parts = append(parts, code[start:i])
				start = i + 1
			}
		}
	}
	var args []string
	kwargs := map[string]string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if eq := kwargIndex(p); eq > 0 {
			kwargs[strings.TrimSpace(p[:eq])] = strings.TrimSpace(p[eq+1:])
			continue
		}
		args = append(args, p)
	}
	return args, kwargs
}

// kwargIndex returns the index of the '=' of `name=value`, or -1.
func kwargIndex(p string) int {
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '=' {
			if i+1 < len(p) && p[i+1] == '=' {
				return -1
			}
			name := strings.TrimSpace(p[:i])
			for j := 0; j < len(name); j++ {
				if !isIdent(name[j]) {
					return -1
				}
			}
			if name == "" {
				return -1
			}
			return i
		}
		if !isIdent(c) && c != ' ' {
			return -1
		}
	}
	return -1
}

// Import is one import statement.
type Import struct {
	Line   int
	Module string   // "sklearn.model_selection" (from X import ...), or the imported module
	Names  []string // from-import names ("train_test_split"), empty for `import X`
	Alias  string   // `import pandas as pd` -> "pd"
}

// PythonImports returns the imports of src.
func PythonImports(src []byte) []Import {
	var out []Import
	for _, l := range PythonLines(src) {
		c := l.Code
		switch {
		case strings.HasPrefix(c, "import "):
			for _, part := range strings.Split(strings.TrimPrefix(c, "import "), ",") {
				f := strings.Fields(part)
				if len(f) == 0 {
					continue
				}
				im := Import{Line: l.Line, Module: f[0]}
				if len(f) == 3 && f[1] == "as" {
					im.Alias = f[2]
				}
				out = append(out, im)
			}
		case strings.HasPrefix(c, "from "):
			rest := strings.TrimPrefix(c, "from ")
			i := strings.Index(rest, " import ")
			if i < 0 {
				continue
			}
			im := Import{Line: l.Line, Module: strings.TrimSpace(rest[:i])}
			names := strings.Trim(strings.TrimSpace(rest[i+8:]), "()")
			for _, n := range strings.Split(names, ",") {
				if f := strings.Fields(n); len(f) > 0 {
					im.Names = append(im.Names, f[0])
				}
			}
			out = append(out, im)
		}
	}
	return out
}
