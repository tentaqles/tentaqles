// Ported from Trail-code src/trace/failure.ts at e02b754c1d4e1de82b841522e2d8cae811702870 (MIT).

package fingerprint

import (
	"encoding/json"
	"io"
	"strings"
)

// emptySignature is the signature of an error text with no line in it.
const emptySignature = "<empty>"

// signatureKeywords are the words that make a line the failure line of rule (c), matched in any case.
var signatureKeywords = []string{
	"error", "fatal", "panic", "exception", "fail", "denied", "not found", "cannot", "unable",
	"invalid", "unknown", "refused", "timed out",
}

// Signature returns the one line that stands for an error text (S-08 step 2): color codes removed, then the
// first rule of (a) to (d) that gives a line, in their order. An empty or blank text gives "<empty>".
func Signature(errText string) string {
	text := stripColorCodes(errText)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return emptySignature
	}
	lines := strings.Split(text, "\n")
	if s, ok := signatureFromJSON(text); ok {
		return s
	}
	if s, ok := signatureFromTraceback(lines); ok {
		return s
	}
	if s, ok := signatureFromFailureLine(lines); ok {
		return s
	}
	return signatureFallback(lines)
}

// signatureFromJSON is rule (a): when the text from its first "{" parses as a JSON object, the text before the
// "{" without its final ":", then status=, code=, type= and error= for each such key that holds text or a number,
// then the message (or msg, detail, title) value. Keys are looked up at the top level, then in "error", then in
// "errors[0]". A parse that gives no part at all falls through to the next rule.
func signatureFromJSON(text string) (string, bool) {
	at := strings.IndexByte(text, '{')
	if at < 0 {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(text[at:]))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return "", false
	}
	// "The text from its first { parses as a JSON object": the whole rest is that one object, with only
	// white space after it. A `{` inside a traceback frame or a sentence is not rule (a).
	if _, err := dec.Token(); err != io.EOF {
		return "", false
	}
	chain := jsonLookupChain(obj)
	var parts []string
	before := strings.TrimSpace(text[:at])
	before = strings.TrimSpace(strings.TrimSuffix(before, ":"))
	if before != "" {
		parts = append(parts, before)
	}
	for _, key := range []string{"status", "code", "type", "error"} {
		if v, ok := jsonScalar(chain, key); ok {
			parts = append(parts, key+"="+v)
		}
	}
	for _, key := range []string{"message", "msg", "detail", "title"} {
		if v, ok := jsonScalar(chain, key); ok {
			parts = append(parts, v)
			break
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " "), true
}

// jsonLookupChain lists the objects a key is looked up in, in order: the top level, "error", "errors[0]".
func jsonLookupChain(obj map[string]any) []map[string]any {
	chain := []map[string]any{obj}
	if inner, ok := obj["error"].(map[string]any); ok {
		chain = append(chain, inner)
	}
	if list, ok := obj["errors"].([]any); ok && len(list) > 0 {
		if first, ok := list[0].(map[string]any); ok {
			chain = append(chain, first)
		}
	}
	return chain
}

// jsonScalar returns the first non-empty text or number under key in the chain. Objects and lists are skipped.
func jsonScalar(chain []map[string]any, key string) (string, bool) {
	for _, m := range chain {
		switch v := m[key].(type) {
		case string:
			if v != "" {
				return v, true
			}
		case json.Number:
			return v.String(), true
		}
	}
	return "", false
}

// signatureFromTraceback is rule (b): after the first line that starts with "Traceback", the last line that starts
// in column 0 (not a space or a tab, and not empty).
func signatureFromTraceback(lines []string) (string, bool) {
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Traceback") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	for i := len(lines) - 1; i > start; i-- {
		line := strings.TrimRight(lines[i], " \t")
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		return line, true
	}
	return "", false
}

// signatureFromFailureLine is rule (c): the first line that holds, in any case, one of the keywords. Stack frames
// and a bare "Exit code N" line are skipped. A stack frame is an indented line that starts with "at " or `File "`.
func signatureFromFailureLine(lines []string) (string, bool) {
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || isBareExitLine(t) || isStackFrame(line) {
			continue
		}
		low := strings.ToLower(t)
		for _, kw := range signatureKeywords {
			if strings.Contains(low, kw) {
				return t, true
			}
		}
	}
	return "", false
}

// signatureFallback is rule (d): the first non-empty line that is not a bare "Exit code N" line; failing that, the
// first bare line; failing that, "<empty>".
func signatureFallback(lines []string) string {
	bare := ""
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if isBareExitLine(t) {
			if bare == "" {
				bare = t
			}
			continue
		}
		return t
	}
	if bare != "" {
		return bare
	}
	return emptySignature
}

// isBareExitLine reports whether a trimmed line is exactly "Exit code N" with N made of digits.
func isBareExitLine(t string) bool {
	rest, ok := strings.CutPrefix(t, "Exit code ")
	if !ok || rest == "" {
		return false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isStackFrame reports whether a raw line is an indented stack frame: it starts with "at " or `File "` after
// its indentation.
func isStackFrame(line string) bool {
	if line == "" || (line[0] != ' ' && line[0] != '\t') {
		return false
	}
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "at ") || strings.HasPrefix(t, `File "`)
}

// stripColorCodes removes ANSI control sequences (ESC [ parameters final-byte), which are color codes in
// practice. A sequence that is not closed is kept as it is.
func stripColorCodes(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
				j++
			}
			if j < len(s) && s[j] >= 0x40 && s[j] <= 0x7e {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
