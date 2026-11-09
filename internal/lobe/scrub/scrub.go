// Package scrub removes secrets from tool inputs and error texts before the lobe
// writes them to its log. A secret becomes a marker `[redacted:<kind>]`, and the
// text is cut to 4000 characters.
package scrub

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// Clean is scrubbed text: the only text the lobe's store accepts. Its field is unexported, so no
// other package can build one without passing through Input or Text.
type Clean struct{ s string }

// String returns the scrubbed text.
func (c Clean) String() string { return c.s }

// MarshalJSON writes the scrubbed text as a JSON string, without HTML escaping.
func (c Clean) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(c.s); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// IsZero reports whether the text is empty, so `omitzero` struct tags drop an empty field.
func (c Clean) IsZero() bool { return c.s == "" }

// ErrUnsafe is returned by Line when the line still holds something that looks like a secret.
const ErrUnsafe = scrubError("scrub: the line still holds something that looks like a secret")

// errNewline is returned by Line when the marshaled line holds a raw newline.
const errNewline = scrubError("scrub: the line holds a raw newline")

// Line marshals v, a record built from Clean values and short fixed metadata, to one line of
// compact JSON without a trailing newline and without HTML escaping. As a last check the whole
// line goes through Detect: a line that still holds something secret-looking is refused with
// ErrUnsafe and nothing is returned.
func Line(v any) (Clean, error) {
	ensure()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return Clean{}, err
	}
	line := strings.TrimSuffix(buf.String(), "\n")
	if strings.Contains(line, "\n") {
		return Clean{}, errNewline
	}
	if _, found := Detect(line); found {
		return Clean{}, ErrUnsafe
	}
	return Clean{line}, nil
}

// unsafeMarker replaces a finished field that still holds something secret-looking.
const unsafeMarker = "[redacted:unsafe]"

// scrubError is an error whose value is a constant, so no package-level error is made by a call
// (this package runs no code at start-up).
type scrubError string

func (e scrubError) Error() string { return string(e) }

// errTrailing reports data after the first JSON value.
const errTrailing = scrubError("scrub: data after the JSON value")

// Text scrubs an error text.
func Text(s string) Clean {
	ensure()
	if s == "" {
		return Clean{}
	}
	if runeLen(s) <= 4000 {
		out, changed := textPass(s)
		if !changed {
			// No rule matched anywhere, so the final check would find nothing either.
			return Clean{s}
		}
		return finishText(capOne(out))
	}
	// Two parts, each scrubbed on its own: the first keeps what is before the cut, the
	// second what is after it. The 256 characters next to each cut are dropped.
	first, _ := textPass(firstRunes(s, 1756))
	first = firstRunes(dropLastRunes(first, 256), 1500)
	second, _ := textPass(lastRunes(s, 2756))
	second = lastRunes(dropFirstRunes(second, 256), 2493)
	return finishText(first + "\n[...]\n" + second)
}

// Input scrubs a tool input given as JSON. Text that is not valid JSON is scrubbed
// as plain text and returned as one JSON string.
func Input(raw json.RawMessage) Clean {
	ensure()
	var v any
	if err := decodeOne(raw, &v); err != nil {
		v = scrubValue(string(raw))
	} else {
		v = scrubTree(v)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return Clean{`"` + unsafeMarker + `"`}
	}
	out := strings.TrimSuffix(buf.String(), "\n")
	if runeLen(out) > 4000 {
		out = firstRunes(out, 3995) + "[...]"
	}
	if _, found := Detect(out); found {
		return Clean{`"` + unsafeMarker + `"`}
	}
	return Clean{out}
}

// Detect reports whether s still holds something that looks like a secret, and its kind.
func Detect(s string) (kind string, found bool) {
	ensure()
	for _, r := range rulesList {
		if len(hits(r, s)) > 0 {
			return r.kind, true
		}
	}
	return "", false
}

// finishText is the last check of a text field: a field that still matches is replaced.
func finishText(s string) Clean {
	if _, found := Detect(s); found {
		return Clean{unsafeMarker}
	}
	return Clean{s}
}

// capOne cuts a single-part text that the markers made longer than 4000 characters.
func capOne(s string) string {
	if runeLen(s) > 4000 {
		return firstRunes(s, 3995) + "[...]"
	}
	return s
}

// textPass applies every rule in order to s, and reports whether any rule replaced something.
func textPass(s string) (string, bool) {
	changed := false
	for _, r := range rulesList {
		if sp := hits(r, s); len(sp) > 0 {
			s = replaceSpans(s, sp, "[redacted:"+r.kind+"]")
			changed = true
		}
	}
	return s, changed
}

// scrubValue is one string value of a tool input: cut to 4256 characters, scrubbed,
// and, when it was cut, its last 256 characters dropped.
func scrubValue(s string) string {
	cut := false
	if runeLen(s) > 4256 {
		s = firstRunes(s, 4256)
		cut = true
	}
	out, _ := textPass(s)
	if cut {
		out = dropLastRunes(out, 256)
	}
	return out
}

// scrubTree applies the key pass and the text pass to every string of a decoded JSON value.
func scrubTree(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if secretKey(k) {
				out[k] = redactScalars(e)
				continue
			}
			out[k] = scrubTree(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		// pending is set when the element before was a secret flag: the element
		// after it is the secret ("secret") or the user:password pair ("user").
		pending := ""
		for i, e := range x {
			s, isStr := e.(string)
			if pending != "" && isStr {
				switch {
				case pending == "secret":
					out[i] = "[redacted:flag]"
				case strings.Contains(s, ":"):
					out[i] = s[:strings.Index(s, ":")+1] + "[redacted:flag]"
				default:
					out[i] = scrubValue(s)
				}
				pending = ""
				continue
			}
			pending = ""
			if isStr {
				pending = flagKind(s)
			}
			out[i] = scrubTree(e)
		}
		return out
	case string:
		return scrubValue(x)
	default:
		return x
	}
}

// flagKind tells whether an array element is a flag whose value is the next element.
func flagKind(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "--password", "--passwd", "--pass", "--token", "--api-key", "--apikey", "--secret", "--client-secret":
		return "secret"
	case "-u", "--user":
		return "user"
	}
	return ""
}

// decodeOne decodes raw as exactly one JSON value, keeping numbers as json.Number.
func decodeOne(raw []byte, v *any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errTrailing
	}
	return nil
}
