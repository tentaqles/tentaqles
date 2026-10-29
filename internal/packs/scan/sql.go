package scan

import (
	"strings"
)

// TokKind is a SQL token kind.
type TokKind int

const (
	TokWord   TokKind = iota // keyword or bare identifier
	TokIdent                 // quoted identifier ("x", `x`, [x]); Text is unquoted
	TokString                // string literal; Text is the contents
	TokNumber
	TokPunct // one operator or punctuation run (, ; . ( ) = <> ...)
	TokJinja // a whole {{ }}, {% %} block, kept opaque (Text includes delimiters)
)

// Token is one SQL token. Comments are dropped.
type Token struct {
	Kind TokKind
	Text string
	Line int
}

// Upper returns the token text upper-cased (for keyword tests).
func (t Token) Upper() string { return strings.ToUpper(t.Text) }

// IsWord reports whether t is the bare word w (case-insensitive).
func (t Token) IsWord(w string) bool { return t.Kind == TokWord && strings.EqualFold(t.Text, w) }

// SQLTokens tokenizes SQL: '...' strings (” escapes), E'...' and
// dollar-quoted $tag$...$tag$ bodies, "..." `...` [...] identifiers, -- and
// /* */ comments (dropped), and Jinja {{ }} {% %} blocks (opaque; {# #} is a
// comment).
func SQLTokens(src []byte) []Token {
	s := string(src)
	var out []Token
	line := 1
	n := len(s)
	for i := 0; i < n; {
		c := s[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			i++
		case c == '-' && i+1 < n && s[i+1] == '-':
			for i < n && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && s[i+1] == '*':
			j := strings.Index(s[i+2:], "*/")
			end := n
			if j >= 0 {
				end = i + 2 + j + 2
			}
			line += strings.Count(s[i:end], "\n")
			i = end
		case c == '{' && i+1 < n && (s[i+1] == '{' || s[i+1] == '%' || s[i+1] == '#'):
			closer := map[byte]string{'{': "}}", '%': "%}", '#': "#}"}[s[i+1]]
			j := strings.Index(s[i+2:], closer)
			end := n
			if j >= 0 {
				end = i + 2 + j + 2
			}
			if s[i+1] != '#' {
				out = append(out, Token{TokJinja, s[i:end], line})
			}
			line += strings.Count(s[i:end], "\n")
			i = end
		case c == '\'' || ((c == 'E' || c == 'e' || c == 'N' || c == 'n') && i+1 < n && s[i+1] == '\'' && !prevIdent(s, i)):
			start := line
			if c != '\'' {
				i++
			}
			j := i + 1
			var b strings.Builder
			for j < n {
				if s[j] == '\'' {
					if j+1 < n && s[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					break
				}
				if s[j] == '\\' && c != '\'' && j+1 < n { // E'' escapes
					b.WriteByte(s[j+1])
					j += 2
					continue
				}
				if s[j] == '\n' {
					line++
				}
				b.WriteByte(s[j])
				j++
			}
			out = append(out, Token{TokString, b.String(), start})
			i = j + 1
		case c == '$':
			if tag, ok := dollarTag(s[i:]); ok {
				body := s[i+len(tag):]
				j := strings.Index(body, tag)
				end := n
				text := body
				if j >= 0 {
					text = body[:j]
					end = i + len(tag) + j + len(tag)
				}
				out = append(out, Token{TokString, text, line})
				line += strings.Count(s[i:end], "\n")
				i = end
				continue
			}
			j := i + 1
			for j < n && isIdent(s[j]) {
				j++
			}
			out = append(out, Token{TokWord, s[i:j], line}) // $1 placeholders
			i = j
		case c == '"' || c == '`':
			j := i + 1
			var b strings.Builder
			for j < n {
				if s[j] == c {
					if j+1 < n && s[j+1] == c {
						b.WriteByte(c)
						j += 2
						continue
					}
					break
				}
				if s[j] == '\n' {
					line++
				}
				b.WriteByte(s[j])
				j++
			}
			out = append(out, Token{TokIdent, b.String(), line})
			i = j + 1
		case c == '[' && !prevValue(out):
			j := strings.IndexByte(s[i:], ']')
			if j < 0 {
				j = n - i
			}
			out = append(out, Token{TokIdent, s[i+1 : i+j], line})
			i += j + 1
		case isIdent(c) && !(c >= '0' && c <= '9'):
			j := i
			for j < n && (isIdent(s[j]) || s[j] == '$' || s[j] >= 0x80) {
				j++
			}
			out = append(out, Token{TokWord, s[i:j], line})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < n && (isIdent(s[j]) || s[j] == '.') {
				j++
			}
			out = append(out, Token{TokNumber, s[i:j], line})
			i = j
		case c >= 0x80:
			j := i
			for j < n && s[j] >= 0x80 {
				j++
			}
			out = append(out, Token{TokWord, s[i:j], line})
			i = j
		default:
			j := i + 1
			if strings.ContainsRune("<>=!|:&", rune(c)) {
				for j < n && strings.ContainsRune("<>=!|:&", rune(s[j])) {
					j++
				}
			}
			out = append(out, Token{TokPunct, s[i:j], line})
			i = j
		}
	}
	return out
}

func prevIdent(s string, i int) bool { return i > 0 && isIdent(s[i-1]) }

// prevValue reports whether the last token can be subscripted (so [ is an
// array subscript, not a T-SQL bracket identifier).
func prevValue(out []Token) bool {
	if len(out) == 0 {
		return false
	}
	t := out[len(out)-1]
	if t.Kind == TokIdent || t.Kind == TokNumber || t.Kind == TokString {
		return true
	}
	if t.Kind == TokPunct && (t.Text == ")" || t.Text == "]") {
		return true
	}
	if t.Kind == TokWord {
		switch strings.ToUpper(t.Text) {
		case "SELECT", "FROM", "JOIN", "ON", "WHERE", "AND", "OR", "BY", "AS", "TABLE", "INTO", "UPDATE", "SET", "TOP", "NOT", "IN", "CREATE", "ALTER", "DROP", "INDEX", "VIEW", "EXEC", "EXECUTE":
			return false
		}
		return true
	}
	return false
}

// dollarTag reads a $tag$ opener at the start of s.
func dollarTag(s string) (string, bool) {
	if len(s) < 2 || s[0] != '$' {
		return "", false
	}
	j := 1
	for j < len(s) && (isIdent(s[j]) && !(j == 1 && s[j] >= '0' && s[j] <= '9')) {
		j++
	}
	if j < len(s) && s[j] == '$' {
		return s[:j+1], true
	}
	return "", false
}

// Statement is one SQL statement (split on top-level semicolons).
type Statement struct {
	Line, EndLine int
	Tokens        []Token
}

// Keyword is the statement's first word, upper-cased ("" for a Jinja-only
// statement).
func (s Statement) Keyword() string {
	for _, t := range s.Tokens {
		if t.Kind == TokWord {
			return t.Upper()
		}
		if t.Kind != TokJinja {
			return ""
		}
	}
	return ""
}

// SQLStatements splits src into statements.
func SQLStatements(src []byte) []Statement {
	var out []Statement
	var cur []Token
	depth := 0
	flush := func(end int) {
		if len(cur) > 0 {
			out = append(out, Statement{Line: cur[0].Line, EndLine: end, Tokens: cur})
		}
		cur = nil
	}
	for _, t := range SQLTokens(src) {
		if t.Kind == TokPunct {
			switch t.Text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			case ";":
				if depth == 0 {
					flush(t.Line)
					continue
				}
			}
		}
		cur = append(cur, t)
	}
	if len(cur) > 0 {
		flush(cur[len(cur)-1].Line)
	}
	return out
}

// Relation is a FROM or JOIN target.
type Relation struct {
	Name  string // dotted name as written, quotes removed ("analytics.orders")
	Line  int
	Jinja bool // the target is a Jinja expression ({{ ref('orders') }}); Name holds the block
}

// Relations returns the FROM and JOIN targets of the statement, including
// the comma-separated members of a FROM list. Subqueries are skipped (their
// own FROMs are found where they occur).
func (s Statement) Relations() []Relation {
	var out []Relation
	toks := s.Tokens
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if !(t.IsWord("FROM") || t.IsWord("JOIN")) {
			continue
		}
		// DELETE FROM / EXTRACT(x FROM y): a FROM inside a function call is not a relation.
		if t.IsWord("FROM") && i > 0 && insideCall(toks, i) {
			continue
		}
		list := t.IsWord("FROM")
		j := i + 1
		for j < len(toks) {
			if toks[j].IsWord("LATERAL") || toks[j].IsWord("ONLY") {
				j++
				continue
			}
			r, next, ok := relationAt(toks, j)
			if !ok {
				break
			}
			out = append(out, r)
			j = next
			// skip an alias
			if j < len(toks) && toks[j].IsWord("AS") {
				j++
			}
			if j < len(toks) && (toks[j].Kind == TokWord || toks[j].Kind == TokIdent) && !reserved(toks[j]) {
				j++
			}
			if list && j < len(toks) && toks[j].Kind == TokPunct && toks[j].Text == "," {
				j++
				continue
			}
			break
		}
	}
	return out
}

func insideCall(toks []Token, i int) bool {
	depth := 0
	for k := i - 1; k >= 0; k-- {
		t := toks[k]
		if t.Kind == TokPunct && t.Text == ")" {
			depth++
		}
		if t.Kind == TokPunct && t.Text == "(" {
			if depth == 0 {
				// "(" preceded by a word that is not a keyword: a function call
				if k > 0 && toks[k-1].Kind == TokWord && !reserved(toks[k-1]) {
					return true
				}
				return false
			}
			depth--
		}
	}
	return false
}

var reservedWords = map[string]bool{"WHERE": true, "JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true, "FULL": true,
	"CROSS": true, "ON": true, "USING": true, "GROUP": true, "ORDER": true, "HAVING": true, "LIMIT": true, "UNION": true, "EXCEPT": true,
	"INTERSECT": true, "WINDOW": true, "QUALIFY": true, "NATURAL": true, "SET": true, "VALUES": true, "SELECT": true, "FROM": true,
	"OFFSET": true, "FETCH": true, "RETURNING": true, "WITH": true, "AS": true, "LATERAL": true, "PIVOT": true, "UNPIVOT": true,
	"SAMPLE": true, "TABLESAMPLE": true, "FOR": true, "INTO": true, "AND": true, "OR": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true}

func reserved(t Token) bool { return t.Kind == TokWord && reservedWords[t.Upper()] }

// relationAt reads a dotted name or a Jinja block at toks[j].
func relationAt(toks []Token, j int) (Relation, int, bool) {
	if j >= len(toks) {
		return Relation{}, j, false
	}
	t := toks[j]
	switch {
	case t.Kind == TokJinja:
		return Relation{Name: t.Text, Line: t.Line, Jinja: true}, j + 1, true
	case t.Kind == TokPunct && t.Text == "(":
		return Relation{}, j, false // subquery
	case (t.Kind == TokWord && !reserved(t)) || t.Kind == TokIdent:
		parts := []string{t.Text}
		k := j + 1
		for k+1 < len(toks) && toks[k].Kind == TokPunct && toks[k].Text == "." && (toks[k+1].Kind == TokWord || toks[k+1].Kind == TokIdent) {
			parts = append(parts, toks[k+1].Text)
			k += 2
		}
		if k < len(toks) && toks[k].Kind == TokPunct && toks[k].Text == "(" {
			return Relation{}, j, false // a table function
		}
		return Relation{Name: strings.Join(parts, "."), Line: t.Line}, k, true
	}
	return Relation{}, j, false
}

// SelectStars returns the lines of every `SELECT *` and `SELECT t.*` in the
// statement (also after DISTINCT and in a select list).
func (s Statement) SelectStars() []int {
	var out []int
	toks := s.Tokens
	inSelect := false
	depth := 0
	selDepth := -1
	for i, t := range toks {
		if t.Kind == TokPunct && t.Text == "(" {
			depth++
		}
		if t.Kind == TokPunct && t.Text == ")" {
			depth--
		}
		if t.IsWord("SELECT") {
			inSelect, selDepth = true, depth
			continue
		}
		if inSelect && depth == selDepth && t.IsWord("FROM") {
			inSelect = false
		}
		if !inSelect || t.Kind != TokPunct || t.Text != "*" {
			continue
		}
		prev := Token{}
		if i > 0 {
			prev = toks[i-1]
		}
		if prev.IsWord("SELECT") || prev.IsWord("DISTINCT") || prev.IsWord("ALL") || (prev.Kind == TokPunct && (prev.Text == "," || prev.Text == ".")) {
			if prev.Kind == TokPunct && prev.Text == "." || !(i+1 < len(toks) && toks[i+1].Kind == TokNumber) {
				out = append(out, t.Line)
			}
		}
	}
	return out
}
