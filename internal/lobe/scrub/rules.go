// Ported from agentmemory src/functions/privacy.ts at 77f45358a8300299158e3a416e9010176408ea87 (Apache-2.0).
// Ported from Trail-code src/notes/secrets.ts at e02b754c1d4e1de82b841522e2d8cae811702870 (MIT).

package scrub

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// span is a byte range [a, b) of the text a rule wants to replace.
type span struct{ a, b int }

// pat finds the spans one pattern wants replaced.
type pat func(s string) []span

// rule is one kind of secret: all its patterns, applied in turn to the text.
type rule struct {
	kind string
	pats []pat
}

var (
	once          sync.Once
	rulesList     []rule
	markerRe      *regexp.Regexp
	envNameRe     *regexp.Regexp
	refRe         *regexp.Regexp
	patternRe     *regexp.Regexp
	driveRe       *regexp.Regexp
	uuidRe        *regexp.Regexp
	uuidAnyRe     *regexp.Regexp
	hexIDRe       *regexp.Regexp
	vendorRe      *regexp.Regexp
	runRe         *regexp.Regexp
	linkRe        *regexp.Regexp
	pieceRe       *regexp.Regexp
	namedRe       *regexp.Regexp
	secretNameRe  *regexp.Regexp
	loginFlagRe   *regexp.Regexp
	cmdWordRe     *regexp.Regexp
	apiRe         *regexp.Regexp
	countSuffixes []string
	openIDs       map[string]bool
)

// providerKeys are the key shapes with a provider prefix, from secrets.ts 31-44 and privacy.ts 31-44.
// The last one is the generic prefix shape; apiKeyAt has its exception.
var providerKeys = []string{
	`[sr]k_(?:live|test)_[A-Za-z0-9]{16,}`,
	`gh[pousr]_[A-Za-z0-9]{20,}`,
	`github_pat_[A-Za-z0-9_]{20,}`,
	`glpat-[A-Za-z0-9_-]{20,}`,
	`npm_[A-Za-z0-9]{30,}`,
	`hf_[A-Za-z0-9]{30,}`,
	`xox[abprs]-[A-Za-z0-9-]{10,}`,
	`(?:AKIA|ASIA)[0-9A-Z]{16}`,
	`AIza[0-9A-Za-z_-]{30,}`,
	`ya29\.[A-Za-z0-9_-]{20,}`,
	`SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}`,
	`GOCSPX-[A-Za-z0-9_-]{20,}`,
	`sb_secret_[A-Za-z0-9_-]{20,}`,
	`sk-proj-[A-Za-z0-9_-]{20,}`,
	`sk-ant-[A-Za-z0-9_-]{20,}`,
	`(?:sk|pk|rk|ak)-[A-Za-z0-9][A-Za-z0-9_-]{19,}`,
}

// telegramKey is a Telegram bot token, often inside its API link (`/bot123:AA…`). It is the
// alternative after providerKeys in apiRe; it needs a non-digit before it.
const telegramKey = `\d{6,12}:AA[A-Za-z0-9_-]{30,}`

// secretNameExpr is the name part of the named rule: a name that holds a secret. The named rule and
// secretKey (keys.go) both use it, so the two name tests cannot drift apart.
const secretNameExpr = `[A-Za-z0-9_.-]*(?:password|passwd|pwd|secret|token|credential|api[_-]?key|private[_-]?key|access[_-]?key|auth[_-]?key)[A-Za-z0-9_-]*`

// build compiles every pattern once; it runs behind sync.Once.
func build() {
	markerRe = regexp.MustCompile(`\[redacted:[a-z-]+\]`)
	envNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+$`)
	// A reference such as process.env.X, os.environ[…], crypto.randomUUID(, or a property .length
	// that ends the call it sits in (`.length)`).
	refRe = regexp.MustCompile(`^[A-Za-z_]\w*(?:\.\w+)+[[(]?$|^[A-Za-z_]\w*(?:\.\w+)*[[(]|^\.[A-Za-z_]\w*[)\]}]*$`)
	// A pattern, not a value: `(.+)`, `[^&]*`, `.*`, `\d+`, `(?:`.
	patternRe = regexp.MustCompile(`^[()[\]|\\^]|\.[*+?{]|\[\^|\\[dswbDSW]|\(\?|\]\{\d`)
	driveRe = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	uuidRe = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	uuidAnyRe = regexp.MustCompile(`\b[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\b`)
	// An id built on a hex digest (`AC` + 32 hex): one case only, with at most five letters first.
	hexIDRe = regexp.MustCompile(`^[A-Za-z]{0,5}[_-]?(?:[0-9a-f]+|[0-9A-F]+)=*$`)
	// A vendor-shaped key: a short prefix, then a random body. The minimum body length is in the pattern:
	// 16 after `_`, 24 after `-`, 32 with no separator. The length is part of the match, so a short
	// candidate does not consume the start of a real key.
	vendorRe = regexp.MustCompile(`\b([a-z]{2,5})(_[A-Za-z0-9]{16,}|-[A-Za-z0-9]{24,}|[A-Za-z0-9]{32,})\b`)
	runRe = regexp.MustCompile(`[A-Za-z0-9_+/=-]{32,}`)
	linkRe = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*://|(?:[a-z0-9-]+\.)+[a-z]{2,}/)[^\s'"<>\x60]*`)
	pieceRe = regexp.MustCompile(`[A-Z]?[a-z]+|[A-Z]+|\d+`)
	// The named rule: the name (group 1), then `=` or `:` with optional quotes and spaces (group 2).
	// The value is read by namedFind.
	namedRe = regexp.MustCompile(`(?i)(` + secretNameExpr + `)(['"]?\s*[=:]\s*)`)
	secretNameRe = regexp.MustCompile(`(?i)` + secretNameExpr)
	// A `-p` flag, with the space or `=` that separates it from its value (group 2). loginPFind
	// reads the command word before it.
	loginFlagRe = regexp.MustCompile(`(^|\s)-p(=|[ \t]+)`)
	cmdWordRe = regexp.MustCompile(`(?:^|\s)(?:login|sshpass)(?:\s|$)`)
	// Every provider key shape, one alternative each; the match of the alternative that matched is
	// the key (apiFind).
	alts := make([]string, 0, len(providerKeys)+1)
	for _, p := range append(append([]string{}, providerKeys...), telegramKey) {
		alts = append(alts, "("+p+")")
	}
	apiRe = regexp.MustCompile(strings.Join(alts, "|"))
	countSuffixes = []string{"id", "ids", "uri", "url", "path", "file", "type", "header", "env", "var",
		"field", "length", "len", "count", "ttl", "expiry", "expires", "expiration", "name", "limit",
		"size", "timeout", "dir", "endpoint", "policy", "rotation", "days", "hours", "seconds"}
	openIDs = map[string]bool{
		"acct": true, "batch": true, "ch": true, "cus": true, "dpl": true, "evt": true, "file": true,
		"in": true, "msg": true, "org": true, "pay": true, "pdt": true, "pi": true, "pm": true, "po": true,
		"price": true, "prj": true, "prod": true, "re": true, "req": true, "run": true, "seti": true,
		"sub": true, "team": true, "toolu": true, "txn": true, "user": true,
	}

	rulesList = []rule{
		{"pem", []pat{rx(regexp.MustCompile(
			`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----[\s\S]*?(?:-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|\z)`),
			0, nil)}},
		// A login in a link: every password is replaced, whatever the host, since a local port can be a
		// tunnel to a real database. Only a stand-in value is left alone (loginPlaceholder).
		{"url-login", []pat{rx(regexp.MustCompile(
			`(?i)[a-z][a-z0-9+.-]*://[^\s:/@'"]+:([^\s@/'"]+)@([^\s/'"?#]*)`), 1,
			func(m []string) bool { return loginPlaceholder(m[1]) })}},
		{"api-key", []pat{apiFind, vendorFind}},
		{"jwt", []pat{rx(regexp.MustCompile(
			`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), 0, nil)}},
		{"bearer", []pat{rx(regexp.MustCompile(
			`(?i)\bbearer\s+([A-Za-z0-9._~+/=-]{8,})`), 1, nil)}},
		{"basic", []pat{rx(regexp.MustCompile(
			`(?i)authorization:\s*basic\s+([A-Za-z0-9+/=]{8,})`), 1, nil)}},
		{"webhook", []pat{
			rx(regexp.MustCompile(`hooks\.slack\.com/(?:services|workflows|triggers)/[A-Za-z0-9_/-]{20,}`), 0, nil),
			rx(regexp.MustCompile(`discord(?:app)?\.com/api/webhooks/\d+/[A-Za-z0-9_-]{20,}`), 0, nil),
		}},
		{"url-key", []pat{rx(regexp.MustCompile(
			`(?i)[?&](?:key|sig|signature|x-amz-signature|x-goog-signature|auth|token|access_token|api_key)=([^&#\s'"]{8,})`),
			1, func(m []string) bool { return placeholder(m[1]) })}},
		{"named", []pat{namedFind}},
		{"flag", []pat{
			rx(regexp.MustCompile(
				`(?i)(?:^|\s)--?(?:password|passwd|pass|token|api-?key|secret|client-secret)(?:=|\s+)['"]?([^\s'"]+)`),
				1, func(m []string) bool { return plainValue(m[1]) }),
			rx(regexp.MustCompile(
				`(?:^|\s)(?:-u|--user)(?:=|\s+)['"]?([A-Za-z0-9_][\w.@-]*):([^\s'"]+)`),
				2, func(m []string) bool { return (allDigits(m[1]) && allDigits(m[2])) || plainValue(m[2]) }),
			loginPFind,
		}},
		{"sql-password", []pat{rx(regexp.MustCompile(
			`(?i)(?:IDENTIFIED BY\s*|\bPASSWORD\s*=\s*|\bPASSWORD\s+)['"]([^'"\n]+)['"]`), 1, nil)}},
		{"random", []pat{randomFind}},
	}
}

// ensure compiles the rule table once.
func ensure() { once.Do(build) }

// rx is a pattern whose match, group g, is the span to replace; skip drops a match.
// A text the pattern does not match at all is answered without the submatch search, which is
// the slow part.
func rx(re *regexp.Regexp, g int, skip func(m []string) bool) pat {
	return func(s string) []span {
		if !re.MatchString(s) {
			return nil
		}
		var out []span
		for _, idx := range re.FindAllStringSubmatchIndex(s, -1) {
			if idx[2*g] < 0 {
				continue
			}
			if skip != nil {
				m := make([]string, len(idx)/2)
				for i := range m {
					if idx[2*i] >= 0 {
						m[i] = s[idx[2*i]:idx[2*i+1]]
					}
				}
				if skip(m) {
					continue
				}
			}
			out = append(out, span{idx[2*g], idx[2*g+1]})
		}
		return out
	}
}

// namedFind finds the values of secret names: the text after `name=` or `name:`. A value in quotes
// is everything up to the next same quote on its line, or to the line's end when there is none.
// Any other value runs to the first whitespace, `&` or quote, less the `,;)]}>` at its end.
func namedFind(s string) []span {
	if !mayName(s) || !namedRe.MatchString(s) {
		return nil
	}
	var out []span
	for _, idx := range namedRe.FindAllStringSubmatchIndex(s, -1) {
		// `${API_KEY:?set it}` reads a variable; it assigns nothing (secrets.ts).
		if idx[0] > 0 && (s[idx[0]-1] == '{' || s[idx[0]-1] == '$') {
			continue
		}
		name := s[idx[2]:idx[3]]
		a := idx[1]
		b := a
		if a < len(s) && (s[a] == '"' || s[a] == '\'') {
			q := s[a]
			a++
			b = a
			for b < len(s) && s[b] != q && s[b] != '\n' {
				b++
			}
		} else {
			for b < len(s) && !plainEnd(s[b]) {
				b++
			}
			for b > a && strings.IndexByte(",;)]}>", s[b-1]) >= 0 {
				b--
			}
		}
		if countName(name) || plainValue(s[a:b]) {
			continue
		}
		out = append(out, span{a, b})
	}
	return out
}

// plainEnd is a byte that ends a value that is not quoted: whitespace, `&`, or a quote.
func plainEnd(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f', '&', '"', '\'':
		return true
	}
	return false
}

// apiFind finds the provider keys in one pass over apiRe. A key starts after a character that is
// not a letter, a digit, a mark or an underscore (a Telegram token: after a non-digit). The generic
// prefix shape is not a key when its body has no digit and no upper-case letter but has a hyphen:
// lower-case words joined by hyphens are a package or command name.
func apiFind(s string) []span {
	if !mayKey(s) || !apiRe.MatchString(s) {
		return nil
	}
	var out []span
	for pos := 0; pos < len(s); {
		idx := apiRe.FindStringSubmatchIndex(s[pos:])
		if idx == nil {
			break
		}
		k := -1
		for g := 1; 2*g+1 < len(idx); g++ {
			if idx[2*g] >= 0 {
				k = g - 1
				break
			}
		}
		a, b := pos+idx[2*k+2], pos+idx[2*k+3]
		if !apiKeyAt(s, k, a, b) {
			pos = a + 1
			continue
		}
		out = append(out, span{a, b})
		pos = b
	}
	return out
}

// mayKey reports whether s could hold a provider key. Each key shape of apiRe contains one of
// `-`, `_`, `.`, `:`, or AKIA, ASIA or AIza, so a text with none of them is skipped without a regex.
func mayKey(s string) bool {
	return strings.ContainsAny(s, "-_.:") || strings.Contains(s, "AKIA") ||
		strings.Contains(s, "ASIA") || strings.Contains(s, "AIza")
}

// mayName reports whether s could hold a secret name: a keyword of namedRe in any case. A text with
// a byte outside ASCII is not judged here, since namedRe's case folding reaches beyond ASCII.
func mayName(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	lower := strings.ToLower(s)
	for _, kw := range []string{"password", "passwd", "pwd", "secret", "token", "credential", "api",
		"private", "access", "auth"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// hasAlnumRun reports whether s has n or more ASCII letters and digits in a row. A vendor key's body
// is such a run, so a text without one holds no vendor key.
func hasAlnumRun(s string, n int) bool {
	run := 0
	for i := 0; i < len(s); i++ {
		if c := s[i]; isDigit(rune(c)) || isLetter(rune(c)) {
			if run++; run >= n {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}

// apiKeyAt reports whether the match [a, b) of alternative k of apiRe is a key in s.
func apiKeyAt(s string, k, a, b int) bool {
	telegram := k == len(providerKeys)
	if a > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:a])
		if telegram {
			if r >= '0' && r <= '9' {
				return false
			}
		} else if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.M, r) || r == '_' {
			return false
		}
	}
	if k == len(providerKeys)-1 {
		// The generic prefix shape: `sk-` (three bytes) then the body.
		body := s[a+3 : b]
		if !hasDigitOrUpper(body) && strings.Contains(body, "-") {
			return false
		}
	}
	return true
}

// hasDigitOrUpper reports whether s has an ASCII digit or an upper-case letter.
func hasDigitOrUpper(s string) bool {
	return countWhere(s, func(r rune) bool { return isDigit(r) || isUpper(r) }) > 0
}

// hits returns the spans of r in s that do not touch a marker already in s.
func hits(r rule, s string) []span {
	var all []span
	for _, p := range r.pats {
		all = append(all, p(s)...)
	}
	if len(all) == 0 {
		return nil
	}
	ms := markerRe.FindAllStringIndex(s, -1)
	var keep []span
	for _, sp := range all {
		if sp.b > sp.a && !touchesAny(sp, ms) {
			keep = append(keep, sp)
		}
	}
	return keep
}

// replaceSpans replaces the union of the spans in s with repl.
func replaceSpans(s string, sp []span, repl string) string {
	if len(sp) == 0 {
		return s
	}
	sort.Slice(sp, func(i, j int) bool { return sp[i].a < sp[j].a })
	var b strings.Builder
	last := 0
	for i := 0; i < len(sp); {
		a, e := sp[i].a, sp[i].b
		j := i + 1
		for j < len(sp) && sp[j].a <= e {
			if sp[j].b > e {
				e = sp[j].b
			}
			j++
		}
		b.WriteString(s[last:a])
		b.WriteString(repl)
		last = e
		i = j
	}
	b.WriteString(s[last:])
	return b.String()
}

func touchesAny(sp span, ms [][]int) bool {
	for _, m := range ms {
		if sp.a < m[1] && m[0] < sp.b {
			return true
		}
	}
	return false
}

func startsInLink(at int, links [][]int) bool {
	for _, l := range links {
		if at >= l[0] && at < l[1] {
			return true
		}
	}
	return false
}

// mayLoginP reports whether s could hold the password flag of a login: a `-p` and a login or sshpass word.
func mayLoginP(s string) bool {
	return strings.Contains(s, "-p") && (strings.Contains(s, "login") || strings.Contains(s, "sshpass"))
}

// loginPFind finds the password of a `-p` flag on a login (`docker login -p`) or after sshpass. The
// command word must come before the flag in the same statement: a `|`, `;`, `&` or newline between
// them ends the statement. The value runs to the next whitespace, or to the closing quote of a quoted
// value (the quotes stay). A placeholder or a value under 4 characters is left alone.
func loginPFind(s string) []span {
	if !mayLoginP(s) {
		return nil
	}
	var out []span
	for _, idx := range loginFlagRe.FindAllStringSubmatchIndex(s, -1) {
		if !cmdWordBefore(s[:idx[3]]) {
			continue
		}
		a := idx[1]
		var b int
		if a < len(s) && (s[a] == '"' || s[a] == '\'') {
			q := s[a]
			a++
			b = len(s)
			if end := strings.IndexAny(s[a:], string(q)+"\n"); end >= 0 {
				b = a + end
			}
		} else {
			b = len(s)
			if end := strings.IndexFunc(s[a:], unicode.IsSpace); end >= 0 {
				b = a + end
			}
		}
		if utf8.RuneCountInString(s[a:b]) < 4 || placeholder(s[a:b]) {
			continue
		}
		out = append(out, span{a, b})
	}
	return out
}

// cmdWordBefore reports whether the statement that the end of prefix belongs to has a login or
// sshpass word before that end.
func cmdWordBefore(prefix string) bool {
	seg := prefix[strings.LastIndexAny(prefix, "|;&\n")+1:]
	return cmdWordRe.MatchString(seg)
}

// plainValue is a value too plain to be a secret: short, a placeholder, another
// variable's name, a path, a reference such as process.env.X, or a pattern.
func plainValue(v string) bool {
	return utf8.RuneCountInString(v) < 4 || placeholder(v) || envNameRe.MatchString(v) ||
		pathLike(v) || refRe.MatchString(v) || patternRe.MatchString(v)
}

// loginPlaceholder is a stand-in URL password, the only kind left alone: it starts with $ (or ${),
// <...>, ***, xxx or ..., or it is empty.
func loginPlaceholder(v string) bool {
	if v == "" {
		return true
	}
	l := strings.ToLower(v)
	for _, p := range []string{"$", "<", "***", "xxx", "..."} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// placeholder is a stand-in value: $TOKEN, ${TOKEN}, `cmd`, %s, <token>, ***, xxx, nn...
func placeholder(v string) bool {
	l := strings.ToLower(v)
	for _, p := range []string{"$", "`", "%", "{", "<", "*", "xxx", "..."} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	// An elided value: the rest is left out with dots or an ellipsis.
	if strings.HasSuffix(l, "...") || strings.HasSuffix(l, "…") {
		return true
	}
	return l == "changeme" || strings.HasPrefix(l, "your") || strings.HasPrefix(l, "example")
}

func pathLike(v string) bool {
	return strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~/") || strings.HasPrefix(v, "./") ||
		strings.HasPrefix(v, "../") || driveRe.MatchString(v) || strings.Contains(v, `\`)
}

// countName is a name about a count or a setting, not a secret: MAX_TOKENS, tokens, TOKEN_TTL, API_KEY_ID.
func countName(name string) bool {
	lower := strings.ToLower(name)
	parts := strings.FieldsFunc(lower, func(r rune) bool { return r == '_' || r == '.' || r == '-' })
	for _, p := range parts {
		if p == "tokens" || p == "max" || p == "min" || p == "num" {
			return true
		}
	}
	if strings.Contains(lower, "tokeniz") {
		return true
	}
	joined := strings.Join(parts, "")
	for _, suf := range countSuffixes {
		if strings.HasSuffix(joined, suf) {
			return true
		}
	}
	return false
}

// vendorFind finds keys shaped like a vendor key: a short prefix, then a long
// mixed body (secrets.ts 278).
func vendorFind(s string) []span {
	// Accepted matches need a body of 16 characters, so a text without such a run is skipped.
	if !hasAlnumRun(s, 16) {
		return nil
	}
	var out []span
	for _, idx := range vendorRe.FindAllStringSubmatchIndex(s, -1) {
		prefix := s[idx[2]:idx[3]]
		tail := s[idx[4]:idx[5]]
		sep, body := "", tail
		if tail[0] == '_' || tail[0] == '-' {
			sep, body = tail[:1], tail[1:]
		}
		if sep != "" && openIDs[prefix] {
			continue
		}
		if ownFingerprint(prefix, sep, body) {
			continue
		}
		mixed := countWhere(body, isDigit) >= 2 && countWhere(body, isLetter) >= 2
		ok := (sep != "" && mixed) ||
			(sep == "" && hasOutsideHex(prefix) && hexOnly(body))
		if ok {
			out = append(out, span{idx[0], idx[1]})
		}
	}
	return out
}

// ownFingerprint reports whether a vendor-shaped match is the memory's own error fingerprint: `fp_` and
// exactly 16 lowercase hex digits (internal/lobe/fingerprint). Only that one shape is let through; any
// other `fp_` value, such as a longer or mixed-case one, is still read as a possible key.
func ownFingerprint(prefix, sep, body string) bool {
	if prefix != "fp" || sep != "_" || len(body) != 16 {
		return false
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// randomFind finds long random runs (32 or more key characters, mixed case and
// digits in short pieces), outside links, not hex digests, UUIDs or paths. A UUID is
// read as a space, so its characters do not join a run (secrets.ts: ids are not keys).
func randomFind(s string) []span {
	text := blankIDs(s)
	runs := runRe.FindAllStringIndex(text, -1)
	if len(runs) == 0 {
		return nil
	}
	links := linkRe.FindAllStringIndex(s, -1)
	var out []span
	for _, idx := range runs {
		if startsInLink(idx[0], links) {
			continue
		}
		run := text[idx[0]:idx[1]]
		if looksLikePath(run) {
			continue
		}
		off := idx[0]
		for _, part := range strings.Split(run, "=") {
			if randomPart(part) {
				out = append(out, span{off, off + len(part)})
			}
			off += len(part) + 1
		}
	}
	return out
}

// blankIDs returns s with every UUID replaced by spaces of the same length, so every
// index into the result is an index into s.
func blankIDs(s string) string {
	if !strings.Contains(s, "-") {
		return s
	}
	ids := uuidAnyRe.FindAllStringIndex(s, -1)
	if len(ids) == 0 {
		return s
	}
	b := []byte(s)
	for _, idx := range ids {
		for i := idx[0]; i < idx[1]; i++ {
			b[i] = ' '
		}
	}
	return string(b)
}

// randomPart is a piece of a run that looks like a random secret.
func randomPart(p string) bool {
	if len(p) < 32 || hexOnly(p) || uuidRe.MatchString(p) || hexIDRe.MatchString(p) {
		return false
	}
	if countWhere(p, isUpper) == 0 || countWhere(p, isLower) == 0 || countWhere(p, isDigit) == 0 {
		return false
	}
	pieces := pieceRe.FindAllString(p, -1)
	if len(pieces) == 0 {
		return false
	}
	total := 0
	for _, pc := range pieces {
		total += len(pc)
	}
	return float64(total)/float64(len(pieces)) < 2.5
}

// looksLikePath is a path of words between slashes, not an opaque blob.
func looksLikePath(run string) bool {
	segs := strings.Split(strings.TrimSuffix(strings.TrimPrefix(run, "/"), "/"), "/")
	if len(segs) < 2 {
		return false
	}
	for _, sg := range segs {
		if sg == "" || len(sg) > 40 || countWhere(sg, isUpper) > 2 {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	return s != "" && countWhere(s, isDigit) == len(s)
}

func hexOnly(s string) bool {
	return s != "" && countWhere(s, func(r rune) bool {
		return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
	}) == len(s)
}

// hasOutsideHex reports whether the prefix has a lower-case letter that hex does not have.
func hasOutsideHex(s string) bool {
	return countWhere(s, func(r rune) bool { return r >= 'g' && r <= 'z' }) > 0
}

func countWhere(s string, f func(rune) bool) int {
	n := 0
	for _, r := range s {
		if f(r) {
			n++
		}
	}
	return n
}

func isDigit(r rune) bool  { return r >= '0' && r <= '9' }
func isUpper(r rune) bool  { return r >= 'A' && r <= 'Z' }
func isLower(r rune) bool  { return r >= 'a' && r <= 'z' }
func isLetter(r rune) bool { return isUpper(r) || isLower(r) }
