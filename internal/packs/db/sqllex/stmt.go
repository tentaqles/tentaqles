package sqllex

import (
	"strings"
)

// Keyword is the upper-cased first word of the statement ("" when it does not
// start with a word).
func (s Statement) Keyword() string {
	if len(s.Tokens) == 0 || s.Tokens[0].Kind != Word {
		return ""
	}
	return s.Tokens[0].Upper()
}

// At is the token at i, or a zero token past the end.
func (s Statement) At(i int) Token {
	if i < 0 || i >= len(s.Tokens) {
		return Token{Kind: Punct}
	}
	return s.Tokens[i]
}

// Match reports whether the tokens from i are the given words, in order and
// case-insensitively. A word "|"-separated ("COLUMN|CONSTRAINT") matches any
// alternative; a word "(" or ")" or "," matches that punctuation.
func (s Statement) Match(i int, words ...string) bool {
	for k, w := range words {
		t := s.At(i + k)
		if !matchWord(t, w) {
			return false
		}
	}
	return true
}

func matchWord(t Token, w string) bool {
	if len(w) == 1 && !isWordStart(w[0]) {
		return t.IsPunct(w)
	}
	for _, alt := range strings.Split(w, "|") {
		if t.Is(alt) {
			return true
		}
	}
	return false
}

// Find returns the index of the first occurrence (at or after from) of the
// word sequence, at any paren depth, or -1.
func (s Statement) Find(from int, words ...string) int {
	for i := from; i < len(s.Tokens); i++ {
		if s.Match(i, words...) {
			return i
		}
	}
	return -1
}

// FindTop is Find restricted to paren depth 0 (relative to from).
func (s Statement) FindTop(from int, words ...string) int {
	depth := 0
	for i := from; i < len(s.Tokens); i++ {
		t := s.Tokens[i]
		if t.IsPunct("(") {
			depth++
		} else if t.IsPunct(")") {
			depth--
		}
		if depth == 0 && s.Match(i, words...) {
			return i
		}
	}
	return -1
}

// Has reports whether the statement contains the word sequence anywhere.
func (s Statement) Has(words ...string) bool { return s.Find(0, words...) >= 0 }

// Skip advances i past any of the optional word sequences, in order: each
// sequence is skipped when it matches at the current position.
func (s Statement) Skip(i int, seqs ...[]string) int {
	for _, seq := range seqs {
		if s.Match(i, seq...) {
			i += len(seq)
		}
	}
	return i
}

// Split splits tokens [from, to) at top-level commas into ranges.
func (s Statement) Split(from, to int) [][2]int {
	if to > len(s.Tokens) || to < 0 {
		to = len(s.Tokens)
	}
	var out [][2]int
	depth, start := 0, from
	for i := from; i < to; i++ {
		t := s.Tokens[i]
		switch {
		case t.IsPunct("("):
			depth++
		case t.IsPunct(")"):
			depth--
		case t.IsPunct(",") && depth == 0:
			out = append(out, [2]int{start, i})
			start = i + 1
		}
	}
	if start < to {
		out = append(out, [2]int{start, to})
	}
	return out
}

// AfterCTE returns the index of the statement keyword after a leading
// common table expression list, WITH [RECURSIVE] name [(cols)] AS [[NOT]
// MATERIALIZED] (…) [, …], or 0 when the statement does not start with one.
func AfterCTE(s Statement) int {
	if !s.Match(0, "WITH") {
		return 0
	}
	k := 1
	if s.Match(k, "RECURSIVE") {
		k++
	}
	for {
		if t := s.At(k); t.Kind != Word && t.Kind != QuotedIdent {
			return 0
		}
		k++ // the CTE name
		if s.At(k).IsPunct("(") {
			if k = s.Close(k); k < 0 {
				return 0
			}
			k++
		}
		if !s.Match(k, "AS") {
			return 0
		}
		k = s.Skip(k+1, []string{"NOT"}, []string{"MATERIALIZED"})
		e := s.Close(k)
		if e < 0 {
			return 0
		}
		k = e + 1
		if s.At(k).IsPunct(",") {
			k++
			continue
		}
		if k >= len(s.Tokens) {
			return 0
		}
		return k
	}
}

// From returns the statement with its tokens from i on (Line, Batch and the
// leading comments kept), for linting the body after a prefix such as a CTE.
func (s Statement) From(i int) Statement {
	out := s
	if i > len(s.Tokens) {
		i = len(s.Tokens)
	}
	out.Tokens = s.Tokens[i:]
	return out
}

// Close returns the index of the ) that closes the ( at i, or -1.
func (s Statement) Close(i int) int {
	if !s.At(i).IsPunct("(") {
		return -1
	}
	depth := 0
	for k := i; k < len(s.Tokens); k++ {
		if s.Tokens[k].IsPunct("(") {
			depth++
		} else if s.Tokens[k].IsPunct(")") {
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return -1
}

// guarded are the statement keywords Unwrap looks for after a T-SQL IF.
var guarded = map[string]bool{"ALTER": true, "CREATE": true, "DROP": true, "UPDATE": true, "DELETE": true,
	"INSERT": true, "EXEC": true, "EXECUTE": true, "TRUNCATE": true, "BEGIN": true, "SET": true}

// Unwrap returns the statement a T-SQL "IF <condition> <statement>" guards
// (for example IF COL_LENGTH(…) IS NULL ALTER TABLE …), keeping the
// leading comments; any other statement is returned unchanged.
func (s Statement) Unwrap() Statement {
	if !s.Match(0, "IF") {
		return s
	}
	depth := 0
	for i := 1; i < len(s.Tokens); i++ {
		t := s.Tokens[i]
		switch {
		case t.IsPunct("("):
			depth++
		case t.IsPunct(")"):
			depth--
		case depth == 0 && t.Kind == Word && guarded[t.Upper()] && !(t.Is("SET") && s.At(i-1).Is("IS", "NOT")):
			out := s
			out.Tokens = s.Tokens[i:]
			out.Line = t.Line
			return out
		}
	}
	return s
}

// Name is a (schema, object) name, folded per dialect.
type Name struct {
	Schema, Object string
}

// String is "schema.object".
func (n Name) String() string {
	if n.Schema == "" {
		return n.Object
	}
	return n.Schema + "." + n.Object
}

// IsZero reports an empty name.
func (n Name) IsZero() bool { return n.Object == "" }

// ParseName parses a dotted name such as "acme.orders", "public.orders" or a
// plain table name into a Name with the dialect's default schema.
func ParseName(s string, d Dialect) Name {
	st := Lex([]byte(s), d)
	if len(st.Statements) == 0 {
		return Name{}
	}
	n, _ := st.Statements[0].Name(0, d)
	return n
}

// Name parses a qualified name at token i (a.b, a.b.c, [a].[b], "a"."b")
// and returns it with the dialect's default schema filled in, plus the index
// after it. A zero Name means there is no name at i.
func (s Statement) Name(i int, d Dialect) (Name, int) {
	var parts []string
	for {
		t := s.At(i)
		v := t.Ident(d)
		if v == "" {
			break
		}
		parts = append(parts, v)
		i++
		if !s.At(i).IsPunct(".") {
			break
		}
		i++
	}
	switch len(parts) {
	case 0:
		return Name{}, i
	case 1:
		return Name{Schema: d.DefaultSchema(), Object: parts[0]}, i
	default:
		return Name{Schema: parts[len(parts)-2], Object: parts[len(parts)-1]}, i
	}
}

// Words returns the upper-cased Word texts of tokens [from, to), other
// tokens as their text: a compact form for messages and type comparison.
func (s Statement) Words(from, to int) []string {
	if to > len(s.Tokens) || to < 0 {
		to = len(s.Tokens)
	}
	var out []string
	for i := from; i < to; i++ {
		out = append(out, s.Tokens[i].Upper())
	}
	return out
}

// Directive is a tq marker in a comment: "tq:<name> <rest>".
// tq:allow PG001: reason → Name "allow", Args ["PG001"], Reason "reason".
// tq:contract expand-0042 → Name "contract", Args ["expand-0042"].
// tq:rls-deny-all reason  → Name "rls-deny-all", Args ["reason"…].
type Directive struct {
	Line   int
	Name   string
	Args   []string
	Reason string
	// HasReason is true when a ": reason" part was present and non-empty.
	HasReason bool
	// Raw is the text after the name.
	Raw string
}

// ParseDirective reads a tq: marker from a comment body.
func ParseDirective(c Comment) (Directive, bool) {
	t := strings.TrimSpace(c.Text)
	if !strings.HasPrefix(strings.ToLower(t), "tq:") {
		return Directive{}, false
	}
	t = t[3:]
	name := t
	rest := ""
	if k := strings.IndexAny(t, " \t:"); k >= 0 {
		name, rest = t[:k], t[k:]
	}
	name = strings.ToLower(name)
	if name == "" {
		return Directive{}, false
	}
	d := Directive{Line: c.Line, Name: name, Raw: strings.TrimSpace(rest)}
	head := rest
	if k := strings.Index(rest, ":"); k >= 0 {
		head = rest[:k]
		d.Reason = strings.TrimSpace(rest[k+1:])
		d.HasReason = d.Reason != ""
	}
	for _, a := range strings.FieldsFunc(head, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		d.Args = append(d.Args, a)
	}
	return d, true
}

// Directives returns the tq: markers in the statement's leading comments.
func (s Statement) Directives() []Directive {
	return directives(s.Leading)
}

// HeaderDirectives returns the tq: markers before the first statement (or
// in the whole file when it has no statement).
func (f *File) HeaderDirectives() []Directive {
	if len(f.Statements) == 0 {
		return directives(f.Comments)
	}
	return directives(f.Statements[0].Leading)
}

func directives(cs []Comment) []Directive {
	var out []Directive
	for _, c := range cs {
		for _, line := range strings.Split(c.Text, "\n") {
			cc := c
			cc.Text = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "*"))
			if d, ok := ParseDirective(cc); ok {
				out = append(out, d)
			}
		}
	}
	return out
}

// Allows reports whether a tq:allow directive among ds names rule, and
// whether that directive has a reason.
func Allows(ds []Directive, rule string) (allowed, reason bool, line int) {
	for _, d := range ds {
		if d.Name != "allow" {
			continue
		}
		for _, a := range d.Args {
			if strings.EqualFold(a, rule) {
				return true, d.HasReason, d.Line
			}
		}
	}
	return false, false, 0
}
