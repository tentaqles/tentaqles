package guard

import "strings"

// heredoc is one `<<WORD` found by scanHeredocs.
type heredoc struct {
	line       int // index of the line that opens it
	start, end int // byte offsets of the `<<WORD` marker in that line
	from, to   int // body lines: [from, to); to is the closing line when closed
	closed     bool
	// odd: the delimiter has a quote or backslash other than one quote
	// pair around the whole word (`<<E"O"F`, `<<\EOF`). The body extent
	// follows bash, but such a body is never judged to be data.
	odd bool
}

// shFrame is one level of bash quoting: 'u' unquoted (the command, or a
// `$( )` inside it), 's' single quotes, 'a' ANSI-C `$'…'`, 'd' double
// quotes, 'b' backticks, 'p' a `${ … }` parameter expansion.
type shFrame struct {
	kind  byte
	depth int // plain parens open inside a 'u' frame
}

// shLexer follows bash quoting byte by byte: quotes, `$( )` (also inside
// double quotes), backticks, backslash escapes and `#` comments. It is
// linear and never fails; on odd input it keeps a best guess.
type shLexer struct {
	st      []shFrame
	comment bool
	esc     bool
	prev    byte
}

func newShLexer() *shLexer { return &shLexer{st: []shFrame{{kind: 'u'}}} }

func (l *shLexer) top() *shFrame { return &l.st[len(l.st)-1] }

func (l *shLexer) push(k byte) { l.st = append(l.st, shFrame{kind: k}) }

func (l *shLexer) pop() {
	if len(l.st) > 1 {
		l.st = l.st[:len(l.st)-1]
	}
}

// live reports whether the next byte is shell code: not quoted, not
// escaped, not in a comment.
func (l *shLexer) live() bool {
	k := l.top().kind
	return !l.comment && !l.esc && (k == 'u' || k == 'b')
}

// root reports whether the next byte is live outside every substitution.
func (l *shLexer) root() bool { return len(l.st) == 1 && l.live() }

func wordStart(prev byte) bool {
	switch prev {
	case 0, ' ', '\t', '\n', '\r', ';', '&', '|', '(':
		return true
	}
	return false
}

// step consumes ch (next is the byte after it, 0 at the end) and returns
// how many extra bytes it consumed (1 for `$(`).
func (l *shLexer) step(ch, next byte) int {
	skip := 0
	switch {
	case l.esc:
		l.esc = false
	case l.comment:
		if ch == '\n' {
			l.comment = false
		}
	default:
		f := l.top()
		switch f.kind {
		case 's':
			if ch == '\'' {
				l.pop()
			}
		case 'a': // $'…': a backslash escapes, an unescaped ' closes
			switch ch {
			case '\\':
				l.esc = true
			case '\'':
				l.pop()
			}
		case 'd':
			switch {
			case ch == '\\':
				l.esc = true
			case ch == '"':
				l.pop()
			case ch == '$' && next == '(':
				l.push('u')
				skip = 1
			case ch == '$' && next == '{':
				l.push('p')
				skip = 1
			case ch == '`':
				l.push('b')
			}
		case 'p': // ${ … }: no marker is read; closes at the matching }
			switch {
			case ch == '\\':
				l.esc = true
			case ch == '\'':
				l.push('s')
			case ch == '"':
				l.push('d')
			case ch == '`':
				l.push('b')
			case ch == '$' && next == '(':
				l.push('u')
				skip = 1
			case ch == '$' && next == '{':
				l.push('p')
				skip = 1
			case ch == '$' && next == '\'':
				l.push('a')
				skip = 1
			case ch == '{':
				f.depth++
			case ch == '}':
				if f.depth > 0 {
					f.depth--
				} else {
					l.pop()
				}
			}
		default:
			switch {
			case ch == '\\':
				l.esc = true
			case ch == '$' && next == '\'':
				l.push('a')
				skip = 1
			case ch == '\'':
				l.push('s')
			case ch == '"':
				l.push('d')
			case ch == '#' && wordStart(l.prev):
				l.comment = true
			case ch == '$' && next == '(':
				l.push('u')
				skip = 1
			case ch == '$' && next == '{':
				l.push('p')
				skip = 1
			case ch == '`':
				if f.kind == 'b' {
					l.pop()
				} else {
					l.push('b')
				}
			case ch == '(':
				f.depth++
			case ch == ')':
				if f.depth > 0 {
					f.depth--
				} else if f.kind == 'u' {
					l.pop()
				}
			}
		}
	}
	l.prev = ch
	if skip == 1 {
		l.prev = next
	}
	return skip
}

// parseMarker reads a heredoc operator at line[k:] (line[k:k+2] == "<<").
// It returns the delimiter as bash sees it (quotes and backslashes
// removed), the end offset of the marker, and whether the delimiter is odd.
func parseMarker(line string, k int) (word string, end int, odd, ok bool) {
	if k > 0 && (line[k-1] == '<' || (line[k-1] >= '0' && line[k-1] <= '9')) {
		return "", 0, false, false // an arithmetic shift, or inside `<<<`
	}
	j := k + 2
	if j < len(line) && line[j] == '<' {
		return "", 0, false, false // a `<<<` here-string
	}
	if j < len(line) && line[j] == '-' {
		j++
	}
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	from := j
	var b strings.Builder
	for j < len(line) {
		ch := line[j]
		if ch == ' ' || ch == '\t' || ch == '\r' || strings.IndexByte(";&|<>()", ch) >= 0 {
			break
		}
		switch ch {
		case '\'', '"':
			e := strings.IndexByte(line[j+1:], ch)
			if e < 0 {
				return "", 0, false, false // unbalanced: no delimiter tq can read
			}
			b.WriteString(line[j+1 : j+1+e])
			j += e + 2
			continue
		case '\\':
			if j+1 < len(line) {
				b.WriteByte(line[j+1])
			}
			j += 2
			continue
		}
		b.WriteByte(ch)
		j++
	}
	if j > len(line) {
		j = len(line)
	}
	raw := line[from:j]
	word = b.String()
	if word == "" {
		return "", 0, false, false
	}
	plain := raw == word
	wrapped := len(raw) == len(word)+2 && (raw[0] == '\'' || raw[0] == '"') &&
		raw[len(raw)-1] == raw[0] && raw[1:len(raw)-1] == word && !strings.ContainsAny(word, `'"\`)
	return word, j, !plain && !wrapped, true
}

// scanHeredocs finds every heredoc in lines the way bash reads them. A `<<`
// inside quotes or a `#` comment opens nothing; a `$( )` inside double
// quotes is code, so `"$(cat <<'EOF'` does. The bodies of several markers
// on one line follow each other, in order; a body is never searched for
// markers and does not change the quoting state. Offsets always refer to
// the line they were found in. Bodies start after the logical line: when
// the opener line ends in a `\` continuation, the next line is still
// command. A line that ends inside an open quote marks its heredocs odd.
// An unterminated heredoc ends the scan. This is the one heredoc reader:
// blankHeredocBodies, StripHeredocs and BlankDataHeredocs all use it.
func scanHeredocs(lines []string) []heredoc {
	var out []heredoc
	var pending []heredoc
	var words []string
	lx := newShLexer()
	for i := 0; i < len(lines); {
		line := lines[i]
		for k := 0; k < len(line); k++ {
			if line[k] == '<' && k+1 < len(line) && line[k+1] == '<' && lx.live() {
				if word, end, odd, ok := parseMarker(line, k); ok {
					pending = append(pending, heredoc{line: i, start: k, end: end, odd: odd})
					words = append(words, word)
					lx.prev = ' '
					k = end - 1
					continue
				}
			}
			var nb byte
			if k+1 < len(line) {
				nb = line[k+1]
			}
			k += lx.step(line[k], nb)
		}
		cont := lx.esc // the line ends in a backslash continuation
		lx.step('\n', 0)
		i++
		if len(pending) == 0 || (cont && i < len(lines)) {
			continue
		}
		openQuote := lx.top().kind != 'u' && lx.top().kind != 'b'
		next := i
		for n, h := range pending {
			h.from, h.to = next, len(lines)
			h.odd = h.odd || openQuote || cont
			for j := next; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == words[n] {
					h.to, h.closed = j, true
					break
				}
			}
			out = append(out, h)
			if !h.closed {
				return out
			}
			next = h.to + 1
		}
		pending, words = nil, nil
		i = next
	}
	for _, h := range pending { // the input ended in a continuation
		h.from, h.to, h.odd = len(lines), len(lines), true
		out = append(out, h)
	}
	return out
}

func blankLines(lines []string, from, to int) {
	for k := from; k < to && k < len(lines); k++ {
		lines[k] = strings.Repeat(" ", len(lines[k]))
	}
}
