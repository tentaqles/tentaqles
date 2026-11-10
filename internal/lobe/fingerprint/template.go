package fingerprint

import (
	"net"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// tplCutLen is the length of a template in characters (Unicode code points), S-08 step 3.
const tplCutLen = 200

// tplQuoteMax is the longest quoted value that mask (3) turns into <q>, in characters.
const tplQuoteMax = 200

// Held text (mask 4) is replaced by a placeholder until mask (7) puts it back. A placeholder is a
// private-use rune, an index rune in plane 15, and a second private-use rune. It holds no ASCII letter or
// digit, so no later mask reads a digit or a keyword in it. Template turns every private-use rune and NUL
// in its input into U+FFFD first, so no input can look like a placeholder.
const (
	tplOpen    = ''
	tplClose   = ''
	tplBase    = 0xF0000
	tplMaxHeld = 0xFFFE // the index runes U+F0000 to U+FFFFD
)

// tplRegexSet holds the regexps that Template needs. They are compiled on first use, inside tplRegexes,
// never at package level. Mask (3) does not use one: a regexp cannot test the character before a quote.
type tplRegexSet struct {
	once     sync.Once
	marker   *regexp.Regexp
	url      *regexp.Regexp
	errCode  *regexp.Regexp
	keyword  *regexp.Regexp
	lineTail *regexp.Regexp
	drive    *regexp.Regexp
	ext      *regexp.Regexp
}

var tplRx tplRegexSet

func tplRegexes() *tplRegexSet {
	tplRx.once.Do(func() {
		tplRx.marker = regexp.MustCompile(`\[redacted:[a-z-]+\]`)
		tplRx.url = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s'"` + "`" + `<>]+`)
		// A keyword, then at least one of spaces, ':', '=', '#' or '(' (checked in Go), then a number or a
		// token of exactly five capital letters and digits. The token is exact so that a capital word after
		// a keyword (`Error: HTTP 404`) is not taken and dropped: the search then finds `HTTP 404`.
		tplRx.keyword = regexp.MustCompile(`(?i:sqlstate|http|status code|status|code|error|errno|exit code|exit status)([ :=#(]*)([0-9]+(?:\.[0-9]+)?|[0-9A-Z]{5})`)
		tplRx.errCode = regexp.MustCompile(`[A-Z]+-?[0-9]{1,6}`)
		tplRx.lineTail = regexp.MustCompile(`(:[0-9]+)+$`)
		tplRx.drive = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
		tplRx.ext = regexp.MustCompile(`\.[A-Za-z0-9]{1,10}$`)
	})
	return &tplRx
}

// Template returns the template of a signature line (S-08 step 3, version fp1): the seven masks in their
// order, then spaces collapsed, the ends trimmed, and a cut at 200 characters. The signature should already
// be one line of the scrubbed error text; Template does not scrub. Equal templates of one tool class give
// one fingerprint.
//
//  1. an S-05 marker [redacted:<kind>] becomes <secret>
//  2. a URL becomes <url:HOST>, HOST being the last two labels of its host name, or ip
//  3. a quoted value ('...', "..." or backticks, at most 200 characters, the opening quote not right after
//     a letter or digit) becomes <q>
//  4. set aside until (7): a number after sqlstate, http, status, status code, code, error, errno, exit code
//     or exit status, and an error code of capital letters, an optional '-' and 1 to 6 digits
//  5. a path becomes <path>
//  6. another token with a digit becomes <n> when it is only digits and . , : + -, else <id>
//  7. the set-aside text is put back, spaces collapse, the ends are trimmed, and the text is cut at 200
func Template(signature string) string {
	h := &tplHold{}
	s := tplClean(signature)
	s = tplRegexes().marker.ReplaceAllLiteralString(s, "<secret>")
	s = tplURLs(s, h)
	s = tplQuotes(s)
	s = tplSetAside(s, h)
	s = tplTokens(s, tplPathToken)
	s = tplTokens(s, tplNumberToken)
	s = h.restore(s)
	s = strings.Join(strings.Fields(s), " ")
	return tplCut(s, tplCutLen)
}

// tplClean makes the input valid UTF-8 and removes NUL and private-use runes, which placeholders use.
func tplClean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	return strings.Map(func(r rune) rune {
		if r == 0 || unicode.Is(unicode.Co, r) {
			return '�'
		}
		return r
	}, s)
}

// tplHold keeps the text that mask (4) sets aside.
type tplHold struct {
	items []string
}

// put stores text and returns its placeholder. It reports false when the table is full, and the caller then
// leaves the text as it is.
func (h *tplHold) put(text string) (string, bool) {
	if len(h.items) >= tplMaxHeld {
		return "", false
	}
	h.items = append(h.items, text)
	return string(tplOpen) + string(rune(tplBase+len(h.items)-1)) + string(tplClose), true
}

// restore replaces every placeholder with the text it holds.
func (h *tplHold) restore(s string) string {
	if len(h.items) == 0 {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		if rs[i] == tplOpen && i+2 < len(rs) && rs[i+2] == tplClose {
			if k := rs[i+1] - tplBase; k >= 0 && int(k) < len(h.items) {
				b.WriteString(h.items[k])
				i += 2
				continue
			}
		}
		b.WriteRune(rs[i])
	}
	return b.String()
}

// tplSubst replaces, in s, the span that fn returns for each match of re. fn gets the whole string and the
// match's byte offsets; returning ok false leaves that match as it is.
func tplSubst(s string, re *regexp.Regexp, fn func(src string, m []int) (lo, hi int, repl string, ok bool)) string {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		lo, hi, repl, ok := fn(s, m)
		if !ok || lo < last {
			continue
		}
		b.WriteString(s[last:lo])
		b.WriteString(repl)
		last = hi
	}
	b.WriteString(s[last:])
	return b.String()
}

// tplURLs is mask (2). The replacement is set aside, so mask (6) cannot read a digit in the host.
func tplURLs(s string, h *tplHold) string {
	return tplSubst(s, tplRegexes().url, func(src string, m []int) (int, int, string, bool) {
		lo, hi := m[0], m[1]
		for hi > lo && strings.IndexByte(".,;:)", src[hi-1]) >= 0 {
			hi--
		}
		host, ok := tplURLHost(src[lo:hi])
		if !ok {
			return 0, 0, "", false
		}
		repl, ok := h.put("<url:" + host + ">")
		return lo, hi, repl, ok
	})
}

// tplURLHost returns HOST for mask (2): ip for an address, else the last two labels of the host name.
func tplURLHost(u string) (string, bool) {
	rest := u[strings.Index(u, "://")+3:]
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	if strings.HasPrefix(rest, "[") {
		return "ip", true
	}
	host := rest
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	if host == "" {
		return "", false
	}
	if net.ParseIP(host) != nil {
		return "ip", true
	}
	labels := strings.Split(host, ".")
	if len(labels) > 2 {
		labels = labels[len(labels)-2:]
	}
	return strings.Join(labels, "."), true
}

// tplQuotes is mask (3), written by hand: an opening quote that is not right after a letter or digit, and a
// closing quote of the same kind within tplQuoteMax characters, become <q>.
func tplQuotes(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if (r == '\'' || r == '"' || r == '`') && (i == 0 || !tplLetterOrDigit(rs[i-1])) {
			if j := tplClosingQuote(rs, i); j >= 0 {
				b.WriteString("<q>")
				i = j
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// tplClosingQuote returns the index of the quote that closes the one at open, or -1.
func tplClosingQuote(rs []rune, open int) int {
	for j := open + 1; j < len(rs) && j-open-1 <= tplQuoteMax; j++ {
		if rs[j] == rs[open] {
			return j
		}
	}
	return -1
}

func tplLetterOrDigit(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// tplSetAside is mask (4): it sets aside error codes and the numbers after a keyword. The text stays the same
// once mask (7) restores it.
func tplSetAside(s string, h *tplHold) string {
	rx := tplRegexes()
	s = tplSubst(s, rx.errCode, func(src string, m []int) (int, int, string, bool) {
		if !tplBoundary(src, m[0], m[1]) {
			return 0, 0, "", false
		}
		repl, ok := h.put(src[m[0]:m[1]])
		return m[0], m[1], repl, ok
	})
	return tplSubst(s, rx.keyword, func(src string, m []int) (int, int, string, bool) {
		sep := src[m[2]:m[3]]
		tok := src[m[4]:m[5]]
		if !tplBoundary(src, m[0], m[5]) || sep == "" {
			return 0, 0, "", false
		}
		if strings.Count(sep, " ") > 3 || len(sep)-strings.Count(sep, " ") > 1 {
			return 0, 0, "", false
		}
		if !tplSetAsideToken(tok) {
			return 0, 0, "", false
		}
		repl, ok := h.put(tok)
		return m[4], m[5], repl, ok
	})
}

// tplSetAsideToken reports whether a token after a keyword is a number (digits, with an optional fraction) or
// a token of five capital letters and digits.
func tplSetAsideToken(tok string) bool {
	intPart, frac, hasFrac := strings.Cut(tok, ".")
	if hasFrac {
		return tplAllDigits(intPart) && tplAllDigits(frac)
	}
	return len(tok) == 5 || tplAllDigits(tok)
}

func tplAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// tplBoundary reports whether the span [lo, hi) is not part of a longer word of letters, digits or '_'.
func tplBoundary(s string, lo, hi int) bool {
	return (lo == 0 || !tplWordByte(s[lo-1])) && (hi == len(s) || !tplWordByte(s[hi]))
}

func tplWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// tplTokens applies fn to every token, a run of non-space characters, and keeps the spaces between them.
func tplTokens(s string, fn func(tok string) string) string {
	var b strings.Builder
	start := -1
	for i, r := range s {
		if unicode.IsSpace(r) {
			if start >= 0 {
				b.WriteString(fn(s[start:i]))
				start = -1
			}
			b.WriteRune(r)
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		b.WriteString(fn(s[start:]))
	}
	return b.String()
}

// tplSplitTail splits off the final ':', ',', '.' or ')' characters of a token, which stay outside a masked
// token.
func tplSplitTail(tok string) (core, tail string) {
	end := len(tok)
	for end > 0 && strings.IndexByte(":,.)", tok[end-1]) >= 0 {
		end--
	}
	return tok[:end], tok[end:]
}

// tplPathToken is mask (5) for one token.
func tplPathToken(tok string) string {
	core, tail := tplSplitTail(tok)
	if core == "" || !tplIsPath(core) {
		return tok
	}
	return "<path>" + tail
}

// tplIsPath reports whether a token (without its final punctuation) is a path: it starts with /, ~/, ./, ../,
// \\ or a drive letter, or it holds a slash and ends in a slash or in a file extension. A :line:column tail is
// not part of the name that is checked.
func tplIsPath(core string) bool {
	rx := tplRegexes()
	base := rx.lineTail.ReplaceAllString(core, "")
	if strings.HasPrefix(base, "/") || strings.HasPrefix(base, "~/") || strings.HasPrefix(base, "./") ||
		strings.HasPrefix(base, "../") || strings.HasPrefix(base, `\\`) || rx.drive.MatchString(base) {
		return true
	}
	cut := strings.LastIndexAny(base, `/\`)
	if cut < 0 {
		return false
	}
	if cut == len(base)-1 {
		return true
	}
	return rx.ext.MatchString(base[cut+1:])
}

// tplNumberToken is mask (6) for one token.
func tplNumberToken(tok string) string {
	core, tail := tplSplitTail(tok)
	if !strings.ContainsAny(core, "0123456789") {
		return tok
	}
	if tplOnlyNumeric(core) {
		return "<n>" + tail
	}
	return "<id>" + tail
}

func tplOnlyNumeric(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && strings.IndexByte(".,:+-", c) < 0 {
			return false
		}
	}
	return true
}

// tplCut returns the first n characters of s.
func tplCut(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
