package model

import (
	"bytes"
	"encoding/json"
	"errors"
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

// maxYAMLLen and maxYAMLNodes cap the work on the YAML path: a
// YAML-looking string over either fails closed to RedactedKey.
const (
	maxYAMLLen   = 1 << 20
	maxYAMLNodes = 1 << 19
)

// yamlKeyLineRe finds a "key:" or "- key:" line, the sign of a YAML
// mapping. The key (group 1) may hold "/" and spaces ("app/password").
var yamlKeyLineRe = regexp.MustCompile(`(?m)^[ \t]*(?:-[ \t]+)*([A-Za-z0-9_./'"-](?:[A-Za-z0-9_./'" -]*[A-Za-z0-9_./'"-])?)[ \t]*:(?:[ \t\r]|$)`)

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

// scrubJSONStream scrubs the JSON values at the start of t, one after
// another (numbers as json.Number); objects and arrays are scrubbed, scalars
// between them are kept. Text after the last one (not JSON) is kept, itself
// scrubbed when it is an encoding. ok is false
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
	for {
		var v any
		if dec.Decode(&v) != nil {
			break // io.EOF, or text that is not JSON: the rest is kept
		}
		end := dec.InputOffset()
		chunk := t[prev:end]
		switch v.(type) {
		case map[string]any, []any:
		default:
			parts = append(parts, chunk) // a scalar between documents: kept
			prev = end
			continue
		}
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
// a "key:" line (also "- key:"), a leading "- " item, or a flow collection.
//
// Each document is decoded into a yaml.Node and converted by plainNode, so
// the work is linear in the input (decoding into Go values runs yaml.v3's
// quadratic duplicate-key check). The work is capped: a YAML-looking string
// longer than maxYAMLLen, or one that expands past maxYAMLNodes values or
// nests deeper than maxEncodedDepth, fails closed to RedactedKey. So does a
// string that does not parse but has a "key:" line with a secret-looking key.
func scrubYAML(s, t string) (string, bool) {
	if !(t[0] == '{' || t[0] == '[' || strings.HasPrefix(t, "- ") || yamlKeyLineRe.MatchString(t)) {
		return "", false
	}
	if len(s) > maxYAMLLen {
		return RedactedKey, true // fail closed
	}
	dec := yaml.NewDecoder(strings.NewReader(s))
	conv := &nodeConv{}
	var docs []any
	containers := 0
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if err == io.EOF {
			break
		}
		if err != nil {
			if secretKeyLine(s) {
				return RedactedKey, true // fail closed
			}
			return "", false
		}
		var root *yaml.Node
		if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
			root = n.Content[0]
		}
		nv, err := conv.value(root, 0)
		if err != nil {
			return RedactedKey, true // over a cap: fail closed
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

// secretKeyLine reports a "key:" line in s whose key looks like a secret.
func secretKeyLine(s string) bool {
	for _, m := range yamlKeyLineRe.FindAllStringSubmatch(s, -1) {
		if SecretKey(strings.Trim(strings.TrimSpace(m[1]), `'"`)) {
			return true
		}
	}
	return false
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

// errYAMLCap: a decoded YAML string is over the depth or value budget.
var errYAMLCap = errors.New("encoded YAML over the depth or value budget")

// nodeConv turns YAML nodes into the shape Scrub walks: maps with string
// keys (other keys in their text form), lists and scalars. n counts the
// values produced, aliases included, so an alias bomb stops at
// maxYAMLNodes instead of expanding.
type nodeConv struct{ n int }

func (c *nodeConv) value(n *yaml.Node, depth int) (any, error) {
	if depth > maxEncodedDepth {
		return nil, errYAMLCap
	}
	if c.n++; c.n > maxYAMLNodes {
		return nil, errYAMLCap
	}
	for i := 0; n != nil && n.Kind == yaml.AliasNode; i++ {
		if i > maxEncodedDepth {
			return nil, errYAMLCap
		}
		n = n.Alias
	}
	if n == nil {
		return nil, nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind == yaml.ScalarNode && k.ShortTag() == "!!merge" {
				// "<<: *a" or "<<: [*a, *b]": keys already set and earlier
				// sources win; the mapping's own later keys overwrite.
				src, err := c.value(n.Content[i+1], depth+1)
				if err != nil {
					return nil, err
				}
				srcs := []any{src}
				if l, ok := src.([]any); ok {
					srcs = l
				}
				for _, x := range srcs {
					mm, _ := x.(map[string]any)
					for mk, mv := range mm {
						if _, set := m[mk]; !set {
							m[mk] = mv
						}
					}
				}
				continue
			}
			key := k.Value
			if k.Kind != yaml.ScalarNode {
				kv, err := c.value(k, depth+1)
				if err != nil {
					return nil, err
				}
				key = fmt.Sprint(kv)
			}
			val, err := c.value(n.Content[i+1], depth+1)
			if err != nil {
				return nil, err
			}
			m[key] = val
		}
		return m, nil
	case yaml.SequenceNode:
		l := make([]any, 0, len(n.Content))
		for _, item := range n.Content {
			val, err := c.value(item, depth+1)
			if err != nil {
				return nil, err
			}
			l = append(l, val)
		}
		return l, nil
	case yaml.ScalarNode:
		switch tag := n.ShortTag(); {
		case tag == "!!str", strings.HasPrefix(tag, "!") && !strings.HasPrefix(tag, "!!"):
			return n.Value, nil // plain text, or a local tag such as !Ref
		default:
			var x any
			if err := n.Decode(&x); err != nil {
				return n.Value, nil
			}
			return x, nil
		}
	}
	return nil, nil
}
