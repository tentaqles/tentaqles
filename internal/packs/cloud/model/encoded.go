package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxEncodedLen bounds the strings scrubEncoded decodes. Longer strings
// still pass through secrets.Redact.
const maxEncodedLen = 4 << 20

// maxEncodedDepth bounds the nesting of a decoded string's value.
const maxEncodedDepth = 64

// yamlKeyLineRe finds a "key:" line, the sign of a YAML mapping.
var yamlKeyLineRe = regexp.MustCompile(`(?m)^\s*[A-Za-z0-9_.'"-]+\s*:(\s|$)`)

// scrubEncoded scrubs a string that encodes JSON objects or arrays (one, or
// several back to back as in JSON lines), or a YAML stream whose documents
// are mappings or lists (block or flow, one line or many), and re-encodes it
// in the same format. ok is false when s is neither. When scrubbing changes
// nothing, s is kept as written (after RedactString), so policy documents
// and the like stay byte for byte.
func scrubEncoded(s string) (string, bool) {
	if len(s) > maxEncodedLen {
		return "", false
	}
	t := strings.TrimSpace(s)
	if t == "" {
		return "", false
	}
	if t[0] == '{' || t[0] == '[' {
		if out, ok := scrubJSONStream(s, t); ok {
			return out, true
		}
		// Not JSON: maybe a YAML flow mapping or sequence ({password: x}).
	}
	return scrubYAML(s, t)
}

const jsonSpace = " \t\r\n"

// scrubJSONStream scrubs the JSON objects and arrays at the start of t, one
// after another (numbers as json.Number). Text after the last one (not JSON,
// or a scalar) is kept, itself scrubbed when it is an encoding. ok is false
// when t does not start with a JSON object or array.
func scrubJSONStream(s, t string) (string, bool) {
	if jsonDepth(t) > maxEncodedDepth {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	var (
		parts   []string // each document with the separator before it
		changed bool
		prev    int64
	)
stream:
	for {
		var v any
		if dec.Decode(&v) != nil {
			break // io.EOF, or text that is not JSON: the rest is kept
		}
		switch v.(type) {
		case map[string]any, []any:
		default:
			break stream // a scalar: kept with the rest
		}
		end := dec.InputOffset()
		chunk := t[prev:end]
		sv := scrub(v, false)
		if reflect.DeepEqual(sv, v) {
			parts = append(parts, chunk)
		} else {
			changed = true
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if enc.Encode(sv) != nil {
				return RedactedKey, true // fail closed
			}
			sep := chunk[:len(chunk)-len(strings.TrimLeft(chunk, jsonSpace))]
			parts = append(parts, sep+strings.TrimSuffix(buf.String(), "\n"))
		}
		prev = end
	}
	if prev == 0 {
		return "", false
	}
	rest := t[prev:]
	if strings.TrimSpace(rest) != "" {
		if r, ok := scrubEncoded(rest); ok && r != RedactString(rest) {
			changed = true
			rest = r
		}
	}
	if !changed {
		return RedactString(s), true
	}
	lead := s[:len(s)-len(strings.TrimLeft(s, jsonSpace))]
	trail := s[len(strings.TrimRight(s, jsonSpace)):]
	return RedactString(lead + strings.Join(parts, "") + rest + trail), true
}

// scrubYAML scrubs a YAML stream whose documents are mappings or lists
// (block or flow style). It is tried only on strings that look like YAML:
// a "key:" line, a leading "- " item, or a flow collection.
func scrubYAML(s, t string) (string, bool) {
	if !(t[0] == '{' || t[0] == '[' || strings.HasPrefix(t, "- ") || yamlKeyLineRe.MatchString(t)) {
		return "", false
	}
	dec := yaml.NewDecoder(strings.NewReader(s))
	var docs []any
	containers := 0
	for {
		var v any
		err := dec.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false
		}
		nv, ok := plainYAML(v, 0)
		if !ok {
			return "", false
		}
		switch nv.(type) {
		case map[string]any, []any:
			containers++
		}
		docs = append(docs, nv)
	}
	if containers == 0 {
		return "", false
	}
	changed := false
	out := make([]any, len(docs))
	for i, d := range docs {
		out[i] = scrub(d, false)
		if !reflect.DeepEqual(out[i], d) {
			changed = true
		}
	}
	if !changed {
		return RedactString(s), true
	}
	enc := make([]string, len(out))
	for i, d := range out {
		b, err := yaml.Marshal(d)
		if err != nil {
			return RedactedKey, true // fail closed
		}
		enc[i] = string(b)
	}
	r := strings.Join(enc, "---\n")
	if !strings.HasSuffix(s, "\n") {
		r = strings.TrimSuffix(r, "\n")
	}
	return RedactString(r), true
}

// jsonDepth is the deepest bracket nesting of t, ignoring brackets in
// strings.
func jsonDepth(t string) int {
	depth, max, inStr, esc := 0, 0, false, false
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{' || c == '[':
			if depth++; depth > max {
				max = depth
			}
		case c == '}' || c == ']':
			depth--
		}
	}
	return max
}

// plainYAML turns decoded YAML into the shape Scrub walks: maps with
// string keys (other keys in their text form), lists and scalars.
func plainYAML(v any, depth int) (any, bool) {
	if depth > maxEncodedDepth {
		return nil, false
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			nx, ok := plainYAML(x, depth+1)
			if !ok {
				return nil, false
			}
			out[k] = nx
		}
		return out, true
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			nx, ok := plainYAML(x, depth+1)
			if !ok {
				return nil, false
			}
			out[fmt.Sprint(k)] = nx
		}
		return out, true
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			nx, ok := plainYAML(x, depth+1)
			if !ok {
				return nil, false
			}
			out[i] = nx
		}
		return out, true
	}
	return v, true
}
