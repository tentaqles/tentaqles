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

// scrubEncoded scrubs a string that encodes a JSON object or array, or a
// multi-line YAML mapping, and re-encodes it in the same format. ok is false
// when s is neither. When scrubbing changes nothing, s is kept as written
// (after RedactString), so policy documents and the like stay byte for byte.
func scrubEncoded(s string) (string, bool) {
	if len(s) > maxEncodedLen {
		return "", false
	}
	t := strings.TrimSpace(s)
	if t == "" {
		return "", false
	}
	if t[0] == '{' || t[0] == '[' {
		v, ok := decodeJSONString(t)
		if !ok {
			return "", false
		}
		sv := scrub(v, false)
		if reflect.DeepEqual(sv, v) {
			return RedactString(s), true
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if enc.Encode(sv) != nil {
			return RedactedKey, true // fail closed
		}
		return RedactString(strings.TrimSuffix(buf.String(), "\n")), true
	}
	if !strings.Contains(t, "\n") || !yamlKeyLineRe.MatchString(t) {
		return "", false
	}
	var v any
	if yaml.Unmarshal([]byte(s), &v) != nil {
		return "", false
	}
	nv, ok := plainYAML(v, 0)
	if !ok {
		return "", false
	}
	if _, isMap := nv.(map[string]any); !isMap {
		return "", false
	}
	sv := scrub(nv, false)
	if reflect.DeepEqual(sv, nv) {
		return RedactString(s), true
	}
	b, err := yaml.Marshal(sv)
	if err != nil {
		return RedactedKey, true // fail closed
	}
	return RedactString(string(b)), true
}

// decodeJSONString decodes exactly one JSON object or array (numbers as
// json.Number) whose nesting stays within maxEncodedDepth.
func decodeJSONString(t string) (any, bool) {
	if jsonDepth(t) > maxEncodedDepth {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false // trailing data: not one document
	}
	switch v.(type) {
	case map[string]any, []any:
		return v, true
	}
	return nil, false
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
