package readers

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"gopkg.in/yaml.v3"
)

// CFN reads a CloudFormation template, JSON or YAML, including SAM
// templates and CDK's cdk.out/*.template.json. One Resource per entry of
// Resources: Addr is the logical id, Type the resource type, After the
// Properties. YAML short-form intrinsics become their long form, as the JSON
// form writes them (!Ref X → {"Ref": X}, !GetAtt A.B → {"Fn::GetAtt":
// ["A","B"]}, !Sub s → {"Fn::Sub": s}, !Condition C → {"Condition": C}), so
// rules see one shape. Meta holds DeletionPolicy, UpdateReplacePolicy,
// Condition and the CDK construct path (Metadata aws:cdk:path).
func CFN(name string, raw []byte, o Options) ([]model.Resource, error) {
	if err := checkSize(raw); err != nil {
		return nil, err
	}
	raw = trimBOM(raw)
	if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '{' {
		d, err := decodeJSON(raw, 2)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		res, ok := cfnResourceMap(asMap(d.root))
		if !ok {
			return nil, fmt.Errorf("%s: %w: not a CloudFormation template", name, ErrUnknownFormat)
		}
		return cfnResources(name, res, func(id string) (int, string) {
			ptr := "/Resources/" + escPointer(id)
			return d.line(ptr), ptr
		}, o), nil
	}
	root, lines, err := parseCFNYAML(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	res, ok := cfnResourceMap(asMap(root))
	if !ok {
		return nil, fmt.Errorf("%s: %w: not a CloudFormation template", name, ErrUnknownFormat)
	}
	return cfnResources(name, res, func(id string) (int, string) { return lines[id], "" }, o), nil
}

// cfnResourceMap returns Resources when the template has one whose entries
// carry an AWS:: (or Custom::, Alexa::) type, or a top-level
// AWSTemplateFormatVersion.
func cfnResourceMap(top map[string]any) (map[string]any, bool) {
	if top == nil {
		return nil, false
	}
	res := asMap(top["Resources"])
	if res == nil {
		if top["AWSTemplateFormatVersion"] != nil {
			return map[string]any{}, true // a template with no resources
		}
		return nil, false
	}
	if top["AWSTemplateFormatVersion"] != nil {
		return res, true
	}
	for _, v := range res {
		if strings.Contains(str(asMap(v)["Type"]), "::") {
			return res, true
		}
	}
	return nil, false
}

func cfnResources(name string, res map[string]any, pos func(string) (int, string), o Options) []model.Resource {
	ids := make([]string, 0, len(res))
	for id := range res {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]model.Resource, 0, len(ids))
	for _, id := range ids {
		m := asMap(res[id])
		if m == nil {
			continue
		}
		line, ptr := pos(id)
		r := model.Resource{
			Addr:   id,
			Type:   str(m["Type"]),
			After:  model.ScrubMap(asMap(m["Properties"])),
			Source: model.KindCFN,
			Env:    o.Env,
			Pos:    model.Pos{File: name, Line: line, Pointer: ptr},
			Meta: meta(
				"deletion_policy", str(m["DeletionPolicy"]),
				"update_replace_policy", str(m["UpdateReplacePolicy"]),
				"condition", str(m["Condition"]),
				"cdk_path", str(model.Get(m, "Metadata", "aws:cdk:path")),
			),
		}
		if r.Type == "" {
			r.Type = "unknown"
		}
		out = append(out, r)
	}
	return out
}

// parseCFNYAML decodes a YAML template into plain Go values (intrinsic
// tags in long form) and returns the line of each Resources key.
func parseCFNYAML(raw []byte) (any, map[string]int, error) {
	var n yaml.Node
	if err := yaml.Unmarshal(raw, &n); err != nil {
		return nil, nil, fmt.Errorf("parse YAML: %w", err)
	}
	if n.Kind != yaml.DocumentNode || len(n.Content) == 0 {
		return nil, nil, fmt.Errorf("parse YAML: empty document")
	}
	root := n.Content[0]
	lines := map[string]int{}
	if root.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == "Resources" {
				rm := deref(root.Content[i+1])
				if rm.Kind == yaml.MappingNode {
					for j := 0; j+1 < len(rm.Content); j += 2 {
						lines[rm.Content[j].Value] = rm.Content[j].Line
					}
				}
			}
		}
	}
	v, err := (&yamlConv{}).value(root, 0)
	if err != nil {
		return nil, nil, err
	}
	return v, lines, nil
}

func deref(n *yaml.Node) *yaml.Node {
	for i := 0; n != nil && n.Kind == yaml.AliasNode && n.Alias != nil && i < 16; i++ {
		n = n.Alias
	}
	return n
}

// maxYAMLDepth bounds recursion; maxYAMLNodes bounds the values produced,
// so an alias bomb (aliases of aliases) fails instead of expanding.
const (
	maxYAMLDepth = 64
	maxYAMLNodes = 1 << 20
)

type yamlConv struct{ n int }

// value converts a node to plain values. A local tag ("!Ref") on any node
// becomes the intrinsic's long form.
func (c *yamlConv) value(n *yaml.Node, depth int) (any, error) {
	if depth > maxYAMLDepth {
		return nil, fmt.Errorf("parse YAML: nesting deeper than %d", maxYAMLDepth)
	}
	if c.n++; c.n > maxYAMLNodes {
		return nil, fmt.Errorf("parse YAML: more than %d values (alias expansion?)", maxYAMLNodes)
	}
	n = deref(n)
	if n == nil {
		return nil, nil
	}
	var v any
	switch n.Kind {
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := deref(n.Content[i])
			if k.Tag == "!!merge" {
				// "<<: *a" or "<<: [*a, *b]": earlier maps win over later
				// ones, and the mapping's own keys win over all of them.
				mv := deref(n.Content[i+1])
				srcs := []*yaml.Node{mv}
				if mv != nil && mv.Kind == yaml.SequenceNode {
					srcs = mv.Content
				}
				for _, src := range srcs {
					merged, err := c.value(src, depth+1)
					if err != nil {
						return nil, err
					}
					mm, ok := merged.(map[string]any)
					if !ok {
						return nil, fmt.Errorf("parse YAML: line %d: merge key needs a mapping or a list of mappings", n.Content[i].Line)
					}
					for mk, x := range mm {
						if _, set := m[mk]; !set {
							m[mk] = x
						}
					}
				}
				continue
			}
			val, err := c.value(n.Content[i+1], depth+1)
			if err != nil {
				return nil, err
			}
			m[k.Value] = val
		}
		v = m
	case yaml.SequenceNode:
		l := make([]any, 0, len(n.Content))
		for _, item := range n.Content {
			val, err := c.value(item, depth+1)
			if err != nil {
				return nil, err
			}
			l = append(l, val)
		}
		v = l
	case yaml.ScalarNode:
		if isLocalTag(n.Tag) {
			v = n.Value
		} else {
			var x any
			if err := n.Decode(&x); err != nil {
				v = n.Value
			} else {
				v = x
			}
		}
	default:
		return nil, nil
	}
	if isLocalTag(n.Tag) {
		return intrinsic(n.Tag[1:], v), nil
	}
	return v, nil
}

func isLocalTag(t string) bool { return strings.HasPrefix(t, "!") && !strings.HasPrefix(t, "!!") }

// intrinsic is the long form of a short-form tag.
func intrinsic(tag string, v any) any {
	switch tag {
	case "Ref", "Condition":
		return map[string]any{tag: v}
	case "GetAtt":
		if s, ok := v.(string); ok {
			if a, b, found := strings.Cut(s, "."); found {
				return map[string]any{"Fn::GetAtt": []any{a, b}}
			}
		}
	}
	return map[string]any{"Fn::" + tag: v}
}
