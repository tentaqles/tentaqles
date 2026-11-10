// Ported from Trail-code src/trace/failure.ts at e02b754c1d4e1de82b841522e2d8cae811702870 (MIT).
// Ported from Trail-code src/trace/checks.ts at e02b754c1d4e1de82b841522e2d8cae811702870 (MIT).

package sense

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
)

// The reading window of S-04 step 1: a text of more than windowAbove code points is read as its first
// windowHead and its last windowTail code points only.
const (
	windowAbove = 4000
	windowHead  = 1500
	windowTail  = 2500
	// errorLines is the most lines of a result text that an error keeps.
	errorLines = 20
	// maxDepth bounds the nesting of result objects and parts that is read.
	maxDepth = 3
)

// Hidden reports whether a call that the host marked as a success failed anyway, and the failing part
// of its output (S-04). The rules run in this order, and the first that fires decides:
//
//   - rule A, any tool: the response is an object whose isError or is_error is true. The error is the
//     result text from its first line on.
//   - rule B, shell tools (Bash, PowerShell): the output holds a test runner's own failing summary or
//     a Go failure line (runnerFailure). The error starts at the first FAIL or --- FAIL line, else at
//     the first failing line. A command whose first program only reads is not judged (owner,
//     2026-11-10).
//   - rule C, Bash only: the command hides its exit code (hidesExit), its first program does not only
//     read (readsOnly), and one line of the output is an error line (exitFailure). The error starts
//     at that line.
//
// The first program of rules B and C is the first statement that is not a cd (firstProgram). The
// error keeps at most 20 lines and loses the line breaks at its end. Hidden reads at most the
// window of the result text, so its cost does not grow with the response. It is pure: no disk, no
// clock. A response it cannot read is a success.
func Hidden(tool string, input, response json.RawMessage) (errText string, hidden bool) {
	pieces, isError := resultPieces(response)
	lines := strings.Split(windowText(pieces), "\n")
	if isError {
		if e := errorFrom(lines, 0); strings.TrimSpace(e) != "" {
			return e, true
		}
		// A flag with no text still fails; a fixed text keeps it apart from an empty error.
		return noErrorText, true
	}
	if !ShellTool(tool) {
		return "", false
	}
	command := shellCommand(NormalInput(input))
	if prog, next := firstProgram(command); readsOnly(prog, next) {
		return "", false
	}
	if i, ok := runnerFailure(lines); ok {
		return errorFrom(lines, i), true
	}
	if tool == "Bash" && hidesExit(command) {
		if i, ok := exitFailure(lines); ok {
			return errorFrom(lines, i), true
		}
	}
	return "", false
}

// responseText is the windowed result text of a response, as Hidden reads it. Tests use it to check
// the window against a full decode.
func responseText(response json.RawMessage) string {
	pieces, _ := resultPieces(response)
	return windowText(pieces)
}

// piece is one stretch of the result text. raw holds its bytes: the inside of a JSON string when
// escaped is true (escapes are still in it), else plain text. nl marks the line break between two
// pieces; it has no bytes.
type piece struct {
	raw     []byte
	escaped bool
	nl      bool
}

// cpSpan is one code point of the result text: the bytes raw[start:end] of pieces[piece]. For a line
// break it is the piece itself.
type cpSpan struct {
	piece      int
	start, end int
}

// resultPieces returns the result text of a response as pieces, and whether the response is an object
// whose isError or is_error is true. A JSON string is the text itself; a list of parts gives the text
// values of its elements; an object gives its stdout and stderr (a shell result, each when not
// empty), else its content read the same way. Any other input is plain text.
func resultPieces(response []byte) ([]piece, bool) {
	v := bytes.TrimSpace(response)
	switch {
	case len(v) == 0:
		return nil, false
	case v[0] == '{':
		parts, isError := objectParts(v, 0)
		return joinPieces(parts), isError
	case v[0] == '"' || v[0] == '[':
		return joinPieces(valueParts(v, 0)), false
	default:
		return joinPieces([]piece{{raw: v}}), false
	}
}

// joinPieces puts a line break between each two non-empty pieces.
func joinPieces(parts []piece) []piece {
	var out []piece
	for _, p := range parts {
		if len(p.raw) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, piece{nl: true})
		}
		out = append(out, p)
	}
	return out
}

// valueParts returns the text pieces of one JSON value: a string, a list of parts (the text of each
// object element), or an object (its shell or content text). A malformed value has none.
func valueParts(v []byte, depth int) []piece {
	if len(v) == 0 || depth > maxDepth {
		return nil
	}
	switch v[0] {
	case '"':
		end, ok := stringEnd(v, 0)
		if !ok {
			return nil
		}
		return []piece{{raw: v[1 : end-1], escaped: true}}
	case '[':
		elems, ok := members(v)
		if !ok {
			return nil
		}
		var out []piece
		for _, e := range elems {
			if len(e.val) == 0 || e.val[0] != '{' {
				continue
			}
			fields, ok := members(e.val)
			if !ok {
				continue
			}
			for _, f := range fields {
				if string(f.key) == "text" {
					out = append(out, valueParts(f.val, depth+1)...)
				}
			}
		}
		return out
	case '{':
		parts, _ := objectParts(v, depth)
		return parts
	}
	return nil
}

// objectParts returns the text pieces of a JSON object and whether its isError or is_error is true.
// An object with stdout or stderr is a shell result: those two, in that order. Any other object gives
// its content.
func objectParts(v []byte, depth int) ([]piece, bool) {
	fields, ok := members(v)
	if !ok {
		return nil, false
	}
	isError, shell := false, false
	for _, f := range fields {
		switch string(f.key) {
		case "isError", "is_error":
			if string(bytes.TrimSpace(f.val)) == "true" {
				isError = true
			}
		case "stdout", "stderr":
			shell = true
		}
	}
	if shell {
		var out []piece
		for _, name := range []string{"stdout", "stderr"} {
			for _, f := range fields {
				if string(f.key) == name {
					out = append(out, valueParts(f.val, depth+1)...)
				}
			}
		}
		return out, isError
	}
	for _, f := range fields {
		if string(f.key) == "content" {
			return valueParts(f.val, depth+1), isError
		}
	}
	return nil, isError
}

// member is one entry of a JSON object (key set) or one element of an array (key nil). val is its
// raw value.
type member struct {
	key []byte
	val []byte
}

// members returns the entries of a JSON object or array. ok is false when v is malformed.
func members(v []byte) (out []member, ok bool) {
	if len(v) < 2 {
		return nil, false
	}
	isObj := v[0] == '{'
	closer := byte(']')
	if isObj {
		closer = '}'
	}
	i := skipSpace(v, 1)
	if i < len(v) && v[i] == closer {
		return out, true
	}
	for {
		var key []byte
		if isObj {
			i = skipSpace(v, i)
			if i >= len(v) || v[i] != '"' {
				return nil, false
			}
			end, ok := stringEnd(v, i)
			if !ok {
				return nil, false
			}
			key = v[i+1 : end-1]
			i = skipSpace(v, end)
			if i >= len(v) || v[i] != ':' {
				return nil, false
			}
			i++
		}
		i = skipSpace(v, i)
		end, ok := valueEnd(v, i)
		if !ok {
			return nil, false
		}
		out = append(out, member{key: key, val: v[i:end]})
		i = skipSpace(v, end)
		if i >= len(v) {
			return nil, false
		}
		switch v[i] {
		case ',':
			i++
		case closer:
			return out, true
		default:
			return nil, false
		}
	}
}

// valueEnd returns the index just after the JSON value that starts at v[i]. Strings are skipped
// without reading their bytes one by one.
func valueEnd(v []byte, i int) (int, bool) {
	if i >= len(v) {
		return 0, false
	}
	switch v[i] {
	case '"':
		return stringEnd(v, i)
	case '{', '[':
		depth := 0
		for j := i; j < len(v); j++ {
			switch v[j] {
			case '"':
				e, ok := stringEnd(v, j)
				if !ok {
					return 0, false
				}
				j = e - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1, true
				}
			}
		}
		return 0, false
	}
	j := i
	for j < len(v) && !isDelim(v[j]) {
		j++
	}
	return j, j > i
}

// stringEnd returns the index just after the JSON string that starts at v[i] (a quote). A quote is
// the closing one when an even number of backslashes stands before it.
func stringEnd(v []byte, i int) (int, bool) {
	j := i + 1
	for {
		k := bytes.IndexByte(v[j:], '"')
		if k < 0 {
			return 0, false
		}
		q := j + k
		n := 0
		for p := q - 1; p > i && v[p] == '\\'; p-- {
			n++
		}
		if n%2 == 0 {
			return q + 1, true
		}
		j = q + 1
	}
}

func skipSpace(v []byte, i int) int {
	for i < len(v) && isSpace(v[i]) {
		i++
	}
	return i
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func isDelim(c byte) bool {
	return isSpace(c) || c == ',' || c == '}' || c == ']'
}

// windowText is the result text of pieces, windowed: the whole text when it has at most 4000 code
// points, else its first 1500 and its last 2500 code points.
func windowText(pieces []piece) string {
	head := firstSpans(pieces, windowAbove+1)
	if len(head) <= windowAbove {
		return render(pieces, head)
	}
	return render(pieces, head[:windowHead]) + render(pieces, lastSpans(pieces, windowTail))
}

// firstSpans returns the first n code points of the pieces, in order, walking forward only as far as
// needed.
func firstSpans(pieces []piece, n int) []cpSpan {
	var out []cpSpan
	for k, p := range pieces {
		if len(out) >= n {
			break
		}
		if p.nl {
			out = append(out, cpSpan{piece: k})
			continue
		}
		for i := 0; i < len(p.raw) && len(out) < n; {
			w := fwdWidth(p.raw, i, p.escaped)
			out = append(out, cpSpan{piece: k, start: i, end: i + w})
			i += w
		}
	}
	return out
}

// lastSpans returns the last n code points of the pieces, in order, walking backward only as far as
// needed.
func lastSpans(pieces []piece, n int) []cpSpan {
	var rev []cpSpan
	for k := len(pieces) - 1; k >= 0 && len(rev) < n; k-- {
		p := pieces[k]
		if p.nl {
			rev = append(rev, cpSpan{piece: k})
			continue
		}
		for e := len(p.raw); e > 0 && len(rev) < n; {
			w := bwdWidth(p.raw, e, p.escaped)
			rev = append(rev, cpSpan{piece: k, start: e - w, end: e})
			e -= w
		}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// render is the text of the code points in spans.
func render(pieces []piece, spans []cpSpan) string {
	var b strings.Builder
	for _, s := range spans {
		p := pieces[s.piece]
		switch {
		case p.nl:
			b.WriteByte('\n')
		case p.escaped:
			b.WriteString(unescapeSpan(p.raw[s.start:s.end]))
		default:
			b.Write(p.raw[s.start:s.end])
		}
	}
	return strings.ToValidUTF8(b.String(), "�")
}

// fwdWidth is the number of bytes of the code point that starts at raw[i]. In an escaped string an
// escape is one code point: a two byte escape takes 2 bytes, `\uXXXX` takes 6, and a high surrogate
// followed by a low one (12 bytes) is one code point.
func fwdWidth(raw []byte, i int, escaped bool) int {
	if !escaped || raw[i] != '\\' {
		_, w := utf8.DecodeRune(raw[i:])
		return w
	}
	if i+1 >= len(raw) {
		return 1
	}
	if raw[i+1] != 'u' {
		return 2
	}
	if i+6 > len(raw) {
		return len(raw) - i
	}
	if hi, ok := uEscapeAt(raw, i); ok && hi >= 0xD800 && hi < 0xDC00 {
		if lo, ok := uEscapeAt(raw, i+6); ok && lo >= 0xDC00 && lo < 0xE000 {
			return 12
		}
	}
	return 6
}

// bwdWidth is the number of bytes of the code point that ends at raw[p-1]; it is fwdWidth read
// backward. A backslash starts an escape when an even number of backslashes stands before it.
func bwdWidth(raw []byte, p int, escaped bool) int {
	if !escaped || raw[p-1] >= 0x80 {
		_, w := utf8.DecodeLastRune(raw[:p])
		return w
	}
	if lo, ok := uEscapeAt(raw, p-6); ok {
		if lo >= 0xDC00 && lo < 0xE000 {
			if hi, ok := uEscapeAt(raw, p-12); ok && hi >= 0xD800 && hi < 0xDC00 {
				return 12
			}
		}
		return 6
	}
	if p >= 2 && raw[p-2] == '\\' && introducer(raw, p-2) {
		return 2
	}
	return 1
}

// introducer reports whether the backslash at raw[q] starts an escape: an even number of backslashes
// stands just before it.
func introducer(raw []byte, q int) bool {
	n := 0
	for k := q - 1; k >= 0 && raw[k] == '\\'; k-- {
		n++
	}
	return n%2 == 0
}

// uEscapeAt returns the code unit of a `\uXXXX` escape that starts at raw[q].
func uEscapeAt(raw []byte, q int) (rune, bool) {
	if q < 0 || q+6 > len(raw) || raw[q] != '\\' || raw[q+1] != 'u' || !introducer(raw, q) {
		return 0, false
	}
	return hex4(raw[q+2 : q+6])
}

// hex4 is the value of four hexadecimal digits.
func hex4(b []byte) (rune, bool) {
	if len(b) != 4 {
		return 0, false
	}
	var v rune
	for _, c := range b {
		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			v = v<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			v = v<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return v, true
}

// unescapeSpan is the text of one code point of an escaped string: a plain character, a two byte
// escape, or a `\uXXXX` escape, with a surrogate pair of 12 bytes joined into one character.
func unescapeSpan(b []byte) string {
	if len(b) < 2 || b[0] != '\\' {
		return string(b)
	}
	switch b[1] {
	case 'n':
		return "\n"
	case 'r':
		return "\r"
	case 't':
		return "\t"
	case 'b':
		return "\b"
	case 'f':
		return "\f"
	case 'u':
		if len(b) < 6 {
			return "�"
		}
		hi, ok := hex4(b[2:6])
		if !ok {
			return "�"
		}
		if !utf16.IsSurrogate(hi) {
			return string(hi)
		}
		if len(b) == 12 {
			if lo, ok := hex4(b[8:12]); ok {
				return string(utf16.DecodeRune(hi, lo))
			}
		}
		return "�"
	}
	return string(b[1:2])
}

// errorFrom is the error text of the lines from line from on: at most errorLines lines, without the
// line breaks at its end.
func errorFrom(lines []string, from int) string {
	end := min(from+errorLines, len(lines))
	return strings.TrimRight(strings.Join(lines[from:end], "\n"), "\r\n")
}

// hiddenPatterns are the compiled patterns of rules B and C. They are set on first use.
type hiddenPatterns struct {
	count, tap, ran, nFailed  *regexp.Regexp
	bracket, exitCode, noSuch *regexp.Regexp
}

var (
	hiddenOnce sync.Once
	hiddenRE   hiddenPatterns
)

// patterns returns the compiled patterns, compiling them on the first call.
func patterns() *hiddenPatterns {
	hiddenOnce.Do(func() {
		hiddenRE = hiddenPatterns{
			// a failing count of 1 or more anywhere in the line: N failed, N errors, N failing
			// The count must not follow a digit or a dot, so `0.5 failed` and `v1.5 failed` are no count.
			count: regexp.MustCompile(`(?:^|[^0-9.])[1-9][0-9]* (?:failed|errors|failing)\b`),
			// TAP: `# fail N` with N not 0
			tap: regexp.MustCompile(`^# fail [1-9][0-9]*\b`),
			// the Python unittest summary line
			ran: regexp.MustCompile(`^Ran [0-9]+ tests? in`),
			// a failing count in a summary line that starts Test Files, Tests or Test Suites:
			nFailed: regexp.MustCompile(`[1-9][0-9]* failed`),
			// rule C: `error[CODE]:` at the start of a line
			bracket: regexp.MustCompile(`^error\[[^\]\s]+\]:`),
			// rule C: `Exit code N` or `exit status N`, N not 0
			exitCode: regexp.MustCompile(`^(?:Exit code|exit status) [1-9][0-9]*`),
			// rule C: `name: ...` ending in command not found, No such file or directory or Permission denied
			noSuch: regexp.MustCompile(`^[^\s:]+: .*(?:command not found|No such file or directory|Permission denied)$`),
		}
	})
	return &hiddenRE
}

// runnerFailure is rule B: the index of the line that decides when a shell output shows a test
// runner's failure or a Go failure line. Each line is read with its white space at the ends taken off.
// A FAILED line counts only when the output has a `Ran N tests in` line. The deciding line is the
// first line starting FAIL or --- FAIL (FAILED included) among the failing lines, else the first
// failing line.
func runnerFailure(lines []string) (int, bool) {
	p := patterns()
	ran := false
	for _, ln := range lines {
		if p.ran.MatchString(strings.Trim(ln, " \t\r")) {
			ran = true
			break
		}
	}
	first, start := -1, -1
	for i, ln := range lines {
		t := strings.Trim(ln, " \t\r")
		if !runnerFailingLine(t, ran, p) {
			continue
		}
		if first < 0 {
			first = i
		}
		if start < 0 && (strings.HasPrefix(t, "FAIL") || strings.HasPrefix(t, "--- FAIL")) {
			start = i
		}
	}
	if start >= 0 {
		return start, true
	}
	return first, first >= 0
}

// runnerFailingLine reports whether one trimmed line is a failure of rule B.
func runnerFailingLine(t string, ran bool, p *hiddenPatterns) bool {
	switch {
	case isGoFail(t):
		return true
	case strings.HasPrefix(t, "FAILED") && ran:
		return true
	case strings.HasPrefix(t, "test result: FAILED"):
		return true
	case p.tap.MatchString(t):
		return true
	case hasSummaryStart(t) && p.nFailed.MatchString(t):
		return true
	}
	return p.count.MatchString(t)
}

// isGoFail reports whether a line is Go test output that failed: a FAIL word at the start, a --- FAIL
// line, or a panic.
func isGoFail(t string) bool {
	if t == "FAIL" || strings.HasPrefix(t, "FAIL ") || strings.HasPrefix(t, "FAIL\t") {
		return true
	}
	return strings.HasPrefix(t, "--- FAIL") || strings.HasPrefix(t, "panic:")
}

// hasSummaryStart reports whether a line starts a test runner's summary: Test Files, Tests or Test Suites:.
func hasSummaryStart(t string) bool {
	return strings.HasPrefix(t, "Test Files") || strings.HasPrefix(t, "Tests") || strings.HasPrefix(t, "Test Suites:")
}

// exitFailure is rule C's line: the index of the first error line of the output. The lines are
// anchored at their start, with a carriage return at the end taken off.
func exitFailure(lines []string) (int, bool) {
	p := patterns()
	for i, ln := range lines {
		if exitLine(strings.TrimRight(ln, "\r"), p) {
			return i, true
		}
	}
	return 0, false
}

// exitLine reports whether one line is an error line of rule C.
func exitLine(l string, p *hiddenPatterns) bool {
	for _, pre := range []string{"fatal:", "error:", "panic:", "npm ERR!", "npm error", "Traceback"} {
		if strings.HasPrefix(l, pre) {
			return true
		}
	}
	return p.bracket.MatchString(l) || p.exitCode.MatchString(l) ||
		strings.Contains(l, "ERR_PNPM_") || p.noSuch.MatchString(l)
}

// noErrorText is the error of a result flagged as an error that carries no text (rule A).
const noErrorText = "the tool flagged an error and gave no text"
