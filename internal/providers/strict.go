package providers

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Strict decoding: a key a provider file's schema does not have is an
// error naming its line and column and the nearest known key, so a typo such as
// `match: { executable: [...] }` (one list silently empty) cannot load.

// keyError is a schema error at a position of the file.
type keyError struct {
	line, col int
	msg       string
}

func (e *keyError) Error() string { return fmt.Sprintf("%d:%d: %s", e.line, e.col, e.msg) }

// checkKnownKeys walks the YAML document against the Provider schema and returns the first
// unknown key. Type errors are left to the decoder.
func checkKnownKeys(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil // the decoder reports it
	}
	return checkKeys(&doc, reflect.TypeOf(Provider{}), "")
}

func schemaFields(t reflect.Type, into map[string]reflect.Type) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		name := strings.Split(tag, ",")[0]
		if name == "-" || !f.IsExported() {
			continue
		}
		if strings.Contains(tag, "inline") {
			ft := f.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				schemaFields(ft, into)
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		into[name] = f.Type
	}
}

func checkKeys(n *yaml.Node, t reflect.Type, where string) error {
	for n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return nil
		}
		fields := map[string]reflect.Type{}
		schemaFields(t, fields)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			ft, ok := fields[k.Value]
			if !ok {
				msg := fmt.Sprintf("unknown key %q", k.Value)
				if where != "" {
					msg += " in " + where
				}
				if s := nearestKey(k.Value, fields); s != "" {
					msg += fmt.Sprintf(" (did you mean %q?)", s)
				}
				return &keyError{line: k.Line, col: k.Column, msg: msg}
			}
			sub := k.Value
			if where != "" {
				sub = where + "." + k.Value
			}
			if err := checkKeys(v, ft, sub); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if n.Kind == yaml.SequenceNode {
			for _, c := range n.Content {
				if err := checkKeys(c, t.Elem(), where); err != nil {
					return err
				}
			}
		}
	case reflect.Map:
		if n.Kind == yaml.MappingNode {
			for i := 1; i < len(n.Content); i += 2 {
				if err := checkKeys(n.Content[i], t.Elem(), where); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// nearestKey is the known key closest to k (edit distance at most 2, or one is a prefix of
// the other), or "".
func nearestKey(k string, fields map[string]reflect.Type) string {
	names := make([]string, 0, len(fields))
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)
	best, bestD := "", 3
	for _, n := range names {
		d := editDistance(strings.ToLower(k), n)
		if strings.HasPrefix(n, strings.ToLower(k)) || strings.HasPrefix(strings.ToLower(k), n) {
			if d > 2 {
				d = 2
			}
		}
		if d < bestD {
			best, bestD = n, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev = cur
	}
	return prev[len(b)]
}
