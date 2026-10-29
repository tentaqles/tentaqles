// Package sqllex is the db pack's SQL lexer (spec D §10). It turns a
// migration file into statements of tokens for two dialects, Postgres and
// T-SQL (SQL Server / Azure SQL), keeping each statement's line and the
// comments that precede it. There is no grammar: rules match token patterns
// per statement.
//
// It handles comments (-- and nested /* */), single-quoted strings (with the
// Postgres E, U&, B and X prefixes and the T-SQL N prefix), quoted
// identifiers ("x" and, in T-SQL, [x]), Postgres dollar quoting ($tag$ …
// $tag$), statement splitting on top-level ;, Postgres BEGIN ATOMIC bodies,
// and for T-SQL the GO batch separator, CREATE PROCEDURE-style bodies that
// run to the end of their batch, and statements without a semicolon.
//
// The package is shared by the migration lint (internal/packs/db/lint), the
// RLS replay (D-DR) and the db gate (D-DG). It never imports internal/packs.
package sqllex

import (
	"regexp"
	"strings"
)

// Dialect is the SQL dialect of a file.
type Dialect int

const (
	// Postgres covers Postgres and Supabase.
	Postgres Dialect = iota
	// TSQL covers SQL Server and Azure SQL.
	TSQL
)

// ParseDialect maps a packs.db.migrations[].engine value to a dialect.
func ParseDialect(engine string) (Dialect, bool) {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "postgres", "postgresql", "supabase":
		return Postgres, true
	case "sqlserver", "mssql", "azuresql", "azure-sql", "tsql":
		return TSQL, true
	}
	return Postgres, false
}

// String is the engine name of the dialect.
func (d Dialect) String() string {
	if d == TSQL {
		return "sqlserver"
	}
	return "postgres"
}

// DefaultSchema is the schema an unqualified name resolves to.
func (d Dialect) DefaultSchema() string {
	if d == TSQL {
		return "dbo"
	}
	return "public"
}

// Kind is a token kind.
type Kind int

const (
	// Word is an unquoted keyword or identifier (T-SQL @var and #temp
	// included).
	Word Kind = iota
	// QuotedIdent is "x" or [x]; Text keeps the delimiters, Value drops them.
	QuotedIdent
	// String is a string literal, including dollar-quoted bodies.
	String
	// Number is a numeric literal or a $1 parameter.
	Number
	// Punct is one punctuation or operator character (:: is one token).
	Punct
)

// Token is one lexical token.
type Token struct {
	Kind Kind
	Text string
	Line int
	// Pos is the byte offset of the token in the file.
	Pos int
}

// Upper is the upper-cased text of a Word, otherwise the text.
func (t Token) Upper() string {
	if t.Kind == Word {
		return strings.ToUpper(t.Text)
	}
	return t.Text
}

// Is reports whether t is a Word equal (case-insensitively) to one of words.
func (t Token) Is(words ...string) bool {
	if t.Kind != Word {
		return false
	}
	for _, w := range words {
		if strings.EqualFold(t.Text, w) {
			return true
		}
	}
	return false
}

// IsPunct reports whether t is the punctuation p.
func (t Token) IsPunct(p string) bool { return t.Kind == Punct && t.Text == p }

// Ident is the identifier value of a Word or QuotedIdent, folded the way the
// dialect folds names: unquoted names lower-case (both dialects compare
// case-insensitively by default), quoted Postgres names kept as written,
// quoted T-SQL names lower-cased. Other kinds return "".
func (t Token) Ident(d Dialect) string {
	switch t.Kind {
	case Word:
		return strings.ToLower(t.Text)
	case QuotedIdent:
		v := unquote(t.Text)
		if d == TSQL {
			return strings.ToLower(v)
		}
		return v
	}
	return ""
}

func unquote(s string) string {
	if len(s) >= 2 {
		switch s[0] {
		case '"':
			return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
		case '[':
			return strings.ReplaceAll(s[1:len(s)-1], `]]`, `]`)
		case '`':
			return s[1 : len(s)-1]
		}
	}
	return s
}

// Comment is one comment; Text is the body without the delimiters, trimmed.
type Comment struct {
	Line, EndLine int
	Pos           int
	Text          string
}

// Statement is one SQL statement.
type Statement struct {
	// Line and EndLine are the lines of the first and last token.
	Line, EndLine int
	// Batch is the T-SQL batch index (GO separated, from 0); always 0 for
	// Postgres.
	Batch int
	// Tokens are the statement's tokens, without the terminating ;.
	Tokens []Token
	// Leading are the comments between the previous statement's end and this
	// statement's first token.
	Leading []Comment
}

// File is a lexed migration file.
type File struct {
	Dialect    Dialect
	Statements []Statement
	// Comments are every comment of the file, in order.
	Comments []Comment
	// GoLines are the lines of the T-SQL GO separators.
	GoLines []int
	// Trailing are the comments after the last statement.
	Trailing []Comment
}

var goLine = regexp.MustCompile(`(?i)^[ \t]*go(?:[ \t]+\d+)?[ \t]*(?:--.*)?\r?$`)

// tsqlStarters start a new T-SQL statement when they are the first token of
// a line at paren depth 0 (T-SQL does not require a semicolon).
var tsqlStarters = map[string]bool{
	"ALTER": true, "CREATE": true, "DROP": true, "UPDATE": true, "DELETE": true, "INSERT": true,
	"EXEC": true, "EXECUTE": true, "SET": true, "BEGIN": true, "COMMIT": true, "ROLLBACK": true,
	"TRUNCATE": true, "DECLARE": true, "IF": true, "PRINT": true, "MERGE": true, "RAISERROR": true,
	"THROW": true, "GRANT": true, "REVOKE": true, "DENY": true, "USE": true, "SAVE": true,
}

// Lex splits src into statements.
func Lex(src []byte, d Dialect) *File {
	l := &lexer{src: string(src), d: d, line: 1, f: &File{Dialect: d}, lineStart: true}
	l.run()
	return l.f
}

type lexer struct {
	src       string
	d         Dialect
	i, line   int
	f         *File
	cur       []Token
	pending   []Comment // comments since the previous statement ended
	batch     int
	depth     int
	lineStart bool      // only whitespace since the last newline
	newLine   bool      // a newline was seen since the last token
	atomic    int       // Postgres BEGIN ATOMIC nesting
	toBatch   bool      // T-SQL: the statement runs to the end of the batch
	since     []Comment // comments since the last token of the current statement
}

func (l *lexer) run() {
	s := l.src
	for l.i < len(s) {
		c := s[l.i]
		switch {
		case c == '\n':
			l.line++
			l.i++
			l.lineStart, l.newLine = true, true
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			l.i++
			continue
		}
		if l.d == TSQL && l.lineStart && (c == 'g' || c == 'G') {
			end := strings.IndexByte(s[l.i:], '\n')
			ln := s[l.i:]
			if end >= 0 {
				ln = s[l.i : l.i+end]
			}
			if goLine.MatchString(ln) {
				l.endStatement()
				l.f.GoLines = append(l.f.GoLines, l.line)
				l.batch++
				l.toBatch = false
				l.depth = 0
				l.i += len(ln)
				continue
			}
		}
		l.lineStart = false
		switch {
		case c == '-' && l.peek(1) == '-':
			l.lineComment()
		case c == '/' && l.peek(1) == '*':
			l.blockComment()
		case c == '\'':
			l.str(l.i, false)
		case (c == 'E' || c == 'e') && l.peek(1) == '\'' && l.d == Postgres:
			l.str(l.i, true)
		case (c == 'N' || c == 'n' || c == 'B' || c == 'b' || c == 'X' || c == 'x') && l.peek(1) == '\'':
			l.str(l.i, false)
		case (c == 'U' || c == 'u') && l.peek(1) == '&' && l.peek(2) == '\'' && l.d == Postgres:
			start := l.i
			l.i++
			l.str(start, false)
		case c == '"':
			l.quoted('"', '"')
		case c == '[' && l.d == TSQL:
			l.quoted('[', ']')
		case c == '`':
			l.quoted('`', '`')
		case c == '$' && l.d == Postgres && l.dollar():
		case c == '$' && isDigit(l.peek(1)):
			start := l.i
			l.i++
			for l.i < len(s) && isDigit(s[l.i]) {
				l.i++
			}
			l.emit(Number, s[start:l.i], start, l.line)
		case isDigit(c) || (c == '.' && isDigit(l.peek(1))):
			l.number()
		case isWordStart(c) || (l.d == TSQL && (c == '@' || c == '#')):
			start := l.i
			l.i++
			for l.i < len(s) && (isWordPart(s[l.i]) || (l.d == TSQL && (s[l.i] == '@' || s[l.i] == '#'))) {
				l.i++
			}
			l.word(s[start:l.i], start)
		case c == ';':
			l.i++
			if l.atomic > 0 || l.toBatch {
				l.emit(Punct, ";", l.i-1, l.line)
				continue
			}
			l.endStatement()
		case c == ':' && l.peek(1) == ':':
			l.emit(Punct, "::", l.i, l.line)
			l.i += 2
		default:
			if c == '(' {
				l.depth++
			} else if c == ')' && l.depth > 0 {
				l.depth--
			}
			l.emit(Punct, string(c), l.i, l.line)
			l.i++
		}
	}
	l.endStatement()
	l.f.Trailing = l.pending
	l.pending = nil
}

func (l *lexer) peek(n int) byte {
	if l.i+n < len(l.src) {
		return l.src[l.i+n]
	}
	return 0
}

func isDigit(c byte) bool     { return c >= '0' && c <= '9' }
func isWordStart(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') || c >= 0x80 }
func isWordPart(c byte) bool  { return isWordStart(c) || isDigit(c) || c == '$' }

func (l *lexer) emit(k Kind, text string, pos, line int) {
	l.cur = append(l.cur, Token{Kind: k, Text: text, Line: line, Pos: pos})
	l.newLine = false
	l.since = nil
}

func (l *lexer) word(w string, pos int) {
	up := strings.ToUpper(w)
	if l.d == TSQL && l.newLine && l.depth == 0 && len(l.cur) > 0 && !l.toBatch && tsqlStarters[up] && l.startsStatement(up) {
		// Comments on the lines between belong to the new statement.
		held := l.since
		l.endStatement()
		l.pending = held
	}
	line := l.line
	l.emit(Word, w, pos, line)
	n := len(l.cur)
	if l.d == Postgres {
		// BEGIN ATOMIC … END (SQL-standard function bodies) holds semicolons.
		if up == "ATOMIC" && n >= 2 && l.cur[n-2].Is("BEGIN") && n > 2 {
			l.atomic++
		} else if up == "END" && l.atomic > 0 {
			l.atomic--
		}
	}
	if l.d == TSQL && n >= 2 && !l.toBatch && l.cur[0].Is("CREATE", "ALTER") {
		// CREATE [OR ALTER] PROCEDURE|FUNCTION|TRIGGER|VIEW must be alone in
		// its batch: everything up to GO is its body.
		k := 1
		if l.cur[k].Is("OR") && n > 3 && l.cur[2].Is("ALTER") {
			k = 3
		}
		if n-1 == k && l.cur[k].Is("PROC", "PROCEDURE", "FUNCTION", "TRIGGER", "VIEW") {
			l.toBatch = true
		}
	}
}

// startsStatement refines the T-SQL statement-start heuristic: a line that
// starts with SET inside an UPDATE, UPDATE/DELETE/INSERT inside a MERGE, or
// ALTER/DROP COLUMN inside an ALTER TABLE continues the statement.
func (l *lexer) startsStatement(up string) bool {
	first := l.cur[0]
	switch {
	case up == "SET" && first.Is("UPDATE"):
		return false
	case (up == "UPDATE" || up == "DELETE" || up == "INSERT") && first.Is("MERGE"):
		return false
	case (up == "ALTER" || up == "DROP") && first.Is("ALTER") && len(l.cur) > 1 && l.cur[1].Is("TABLE"):
		// Decided after the next word; peek it from the source.
		rest := strings.TrimLeft(l.src[l.i:], " \t\r\n")
		nw := rest
		for j := 0; j < len(rest); j++ {
			if !isWordPart(rest[j]) {
				nw = rest[:j]
				break
			}
		}
		switch strings.ToUpper(nw) {
		case "COLUMN", "CONSTRAINT", "PERIOD", "INDEX":
			if up == "DROP" && strings.EqualFold(nw, "INDEX") {
				return true
			}
			return false
		}
	}
	return true
}

func (l *lexer) endStatement() {
	if len(l.cur) == 0 {
		l.toBatch = false
		return
	}
	st := Statement{Line: l.cur[0].Line, EndLine: l.cur[len(l.cur)-1].Line, Batch: l.batch, Tokens: l.cur, Leading: l.pending}
	l.f.Statements = append(l.f.Statements, st)
	l.cur, l.pending, l.since = nil, nil, nil
	l.depth, l.atomic, l.toBatch = 0, 0, false
}

func (l *lexer) addComment(c Comment) {
	l.f.Comments = append(l.f.Comments, c)
	if len(l.cur) == 0 {
		l.pending = append(l.pending, c)
	} else {
		l.since = append(l.since, c)
	}
}

func (l *lexer) lineComment() {
	start := l.i
	end := strings.IndexByte(l.src[l.i:], '\n')
	if end < 0 {
		l.i = len(l.src)
	} else {
		l.i += end
	}
	l.addComment(Comment{Line: l.line, EndLine: l.line, Pos: start, Text: strings.TrimSpace(strings.TrimSuffix(l.src[start+2:l.i], "\r"))})
}

func (l *lexer) blockComment() {
	start, line := l.i, l.line
	l.i += 2
	depth := 1
	for l.i < len(l.src) && depth > 0 {
		switch {
		case l.src[l.i] == '/' && l.peek(1) == '*':
			depth++
			l.i += 2
		case l.src[l.i] == '*' && l.peek(1) == '/':
			depth--
			l.i += 2
		default:
			if l.src[l.i] == '\n' {
				l.line++
			}
			l.i++
		}
	}
	body := l.src[start+2 : l.i]
	body = strings.TrimSuffix(body, "*/")
	l.addComment(Comment{Line: line, EndLine: l.line, Pos: start, Text: strings.TrimSpace(body)})
}

// str lexes a quoted string starting at the quote at l.i (prefix letters at
// start are included in the token text).
func (l *lexer) str(start int, backslash bool) {
	line := l.line
	for l.i < len(l.src) && l.src[l.i] != '\'' {
		l.i++
	}
	l.i++ // opening quote
	for l.i < len(l.src) {
		c := l.src[l.i]
		if c == '\n' {
			l.line++
		}
		if backslash && c == '\\' {
			l.i += 2
			continue
		}
		if c == '\'' {
			if l.peek(1) == '\'' {
				l.i += 2
				continue
			}
			l.i++
			break
		}
		l.i++
	}
	if l.i > len(l.src) {
		l.i = len(l.src)
	}
	l.emit(String, l.src[start:l.i], start, line)
}

func (l *lexer) quoted(open, close byte) {
	start, line := l.i, l.line
	l.i++
	for l.i < len(l.src) {
		c := l.src[l.i]
		if c == '\n' {
			l.line++
		}
		if c == close {
			if l.peek(1) == close {
				l.i += 2
				continue
			}
			l.i++
			break
		}
		l.i++
	}
	l.emit(QuotedIdent, l.src[start:l.i], start, line)
}

// dollar lexes $tag$ … $tag$ at l.i; false when it is not a dollar quote.
func (l *lexer) dollar() bool {
	s := l.src
	j := l.i + 1
	for j < len(s) && (isWordStart(s[j]) || (j > l.i+1 && isDigit(s[j]))) {
		j++
	}
	if j >= len(s) || s[j] != '$' {
		return false
	}
	tag := s[l.i : j+1]
	end := strings.Index(s[j+1:], tag)
	start, line := l.i, l.line
	if end < 0 {
		l.i = len(s)
	} else {
		l.i = j + 1 + end + len(tag)
	}
	l.line += strings.Count(s[start:l.i], "\n")
	l.emit(String, s[start:l.i], start, line)
	return true
}

func (l *lexer) number() {
	start := l.i
	s := l.src
	for l.i < len(s) && (isDigit(s[l.i]) || s[l.i] == '.' || s[l.i] == '_') {
		l.i++
	}
	if l.i < len(s) && (s[l.i] == 'e' || s[l.i] == 'E') {
		k := l.i + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		if k < len(s) && isDigit(s[k]) {
			l.i = k
			for l.i < len(s) && isDigit(s[l.i]) {
				l.i++
			}
		}
	}
	l.emit(Number, s[start:l.i], start, l.line)
}
