package scan

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseYAML parses one YAML document and returns its root node (a mapping,
// a sequence or a scalar); an empty document is an empty mapping. Line
// numbers are kept on every node.
func ParseYAML(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return resolveAlias(doc.Content[0]), nil
	}
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: 1}, nil
}

// ParseYAMLAll parses every document of a multi-document stream.
func ParseYAMLAll(data []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	var out []*yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return out, err
		}
		if len(doc.Content) > 0 {
			out = append(out, resolveAlias(doc.Content[0]))
		}
	}
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// Get walks mapping keys (and, for a numeric segment, sequence indexes) from
// n; nil when a step is missing.
func Get(n *yaml.Node, path ...string) *yaml.Node {
	n = resolveAlias(n)
	for _, k := range path {
		if n == nil {
			return nil
		}
		switch n.Kind {
		case yaml.MappingNode:
			var next *yaml.Node
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == k {
					next = n.Content[i+1]
				}
			}
			n = resolveAlias(next)
		case yaml.SequenceNode:
			var idx int
			if _, err := fmt.Sscanf(k, "%d", &idx); err != nil || idx < 0 || idx >= len(n.Content) {
				return nil
			}
			n = resolveAlias(n.Content[idx])
		default:
			return nil
		}
	}
	return n
}

// KeyValue is one mapping entry with the key's line.
type KeyValue struct {
	Key   string
	Line  int
	Value *yaml.Node
}

// Entries returns a mapping's entries in order (nil for a non-mapping).
func Entries(n *yaml.Node) []KeyValue {
	n = resolveAlias(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]KeyValue, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, KeyValue{Key: n.Content[i].Value, Line: n.Content[i].Line, Value: resolveAlias(n.Content[i+1])})
	}
	return out
}

// Items returns a sequence's items (a scalar becomes a one-item list).
func Items(n *yaml.Node) []*yaml.Node {
	n = resolveAlias(n)
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.SequenceNode:
		out := make([]*yaml.Node, len(n.Content))
		for i, c := range n.Content {
			out[i] = resolveAlias(c)
		}
		return out
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return nil
		}
		return []*yaml.Node{n}
	}
	return nil
}

// Scalar returns a scalar's value, "" otherwise.
func Scalar(n *yaml.Node) string {
	n = resolveAlias(n)
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return ""
	}
	return n.Value
}

// Strings returns the scalar values of a sequence (or a single scalar).
func Strings(n *yaml.Node) []string {
	var out []string
	for _, it := range Items(n) {
		if it.Kind == yaml.ScalarNode {
			out = append(out, it.Value)
		}
	}
	return out
}

// Line is the node's 1-based line (0 for nil).
func Line(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	return n.Line
}

// Walk visits every node depth-first with its key path (sequence indexes as
// "[i]"). fn returns false to skip a node's children.
func Walk(n *yaml.Node, fn func(path []string, n *yaml.Node) bool) {
	var rec func(path []string, n *yaml.Node)
	rec = func(path []string, n *yaml.Node) {
		n = resolveAlias(n)
		if n == nil || !fn(path, n) {
			return
		}
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				rec(append(append([]string(nil), path...), n.Content[i].Value), n.Content[i+1])
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				rec(append(append([]string(nil), path...), fmt.Sprintf("[%d]", i)), c)
			}
		}
	}
	rec(nil, n)
}
