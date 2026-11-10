package settings

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"gopkg.in/yaml.v3"
)

// Edit returns the manifest raw with lobe.<key> set to value (F-06 step 5). value is one of the key's
// values, written exactly, or "default": that removes the key, and then the lobe mapping when it is empty.
// Every other key, value and comment stays. The edit works on the YAML node tree, and a new lobe mapping is
// added as the last top-level key. When the key already holds value, Edit returns raw unchanged (the same
// bytes). A lobe value that is not a mapping is an error: Edit does not overwrite it. The result is checked by
// manifest.Parse before it is returned.
func Edit(raw []byte, key, value string) ([]byte, error) {
	if !Known(key) {
		return nil, fmt.Errorf("unknown setting %q (known: %s)", key, strings.Join(Keys(), ", "))
	}
	if value != "default" && !ValidValue(key, value) {
		return nil, fmt.Errorf("%s takes %s or default, not %q", key, Hint(key), value)
	}
	if _, err := manifest.Parse(raw, "the manifest"); err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the manifest is not a YAML mapping")
	}
	top := doc.Content[0]
	lobe := lookup(top, "lobe")
	if lobe != nil && isNull(lobe) {
		lobe = nil // `lobe:` with nothing after it is absent, as it is when the manifest is read
	}
	if lobe != nil && lobe.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the lobe key of the manifest is not a mapping; fix it by hand")
	}

	if value == "default" {
		if lobe == nil || lookup(lobe, key) == nil {
			return raw, nil
		}
		deleteKey(lobe, key)
		if len(lobe.Content) == 0 {
			deleteKey(top, "lobe")
		}
	} else {
		if cur := lookup(lobe, key); cur != nil && cur.Kind == yaml.ScalarNode {
			if c, ok := canonical(key, cur.Value); ok && c == value {
				return raw, nil
			}
		}
		if lobe == nil {
			lobe = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setNode(top, "lobe", lobe)
		}
		setNode(lobe, key, scalarNode(key, value))
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(4)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	if _, err := manifest.Parse(buf.Bytes(), "the edited manifest"); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// scalarNode is the node for a value. retention_days is a number. on and off are written in double quotes:
// YAML 1.1 readers take a bare on or off as a boolean (F-06 "How to build it"). The other values are plain.
func scalarNode(key, value string) *yaml.Node {
	if key == RetentionDays {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: value}
	}
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	if value == "on" || value == "off" {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// setNode sets key in mapping m: in place when the key is there (its comments kept), else appended.
func setNode(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v.HeadComment, v.LineComment = m.Content[i+1].HeadComment, m.Content[i+1].LineComment
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

// deleteKey removes key from mapping m, if it is there.
func deleteKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}
