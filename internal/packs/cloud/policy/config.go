// Package policy is the cloud pack's IaC policy (spec D §7): the built-in
// IAC0xx rules over the shared Resource model, the inputs they read (IaC
// source under packs.cloud.iac[].path and recorded plans), the
// cloud/no-plan-files check, and the Rego bundle shipped for Conftest.
package policy

import (
	"fmt"
	"path"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

// IaC kinds (packs.cloud.iac[].kind).
const (
	KindTerraform      = "terraform"
	KindTofu           = "tofu"
	KindBicep          = "bicep"
	KindARM            = "arm"
	KindCDK            = "cdk"
	KindCloudFormation = "cloudformation"
	KindPulumi         = "pulumi"
)

// Kinds lists the accepted IaC kinds.
var Kinds = []string{KindTerraform, KindTofu, KindBicep, KindARM, KindCDK, KindCloudFormation, KindPulumi}

// Entry is one packs.cloud.iac item.
type Entry struct {
	Path string `yaml:"path"`
	Kind string `yaml:"kind"`
}

// Config is the part of packs.cloud this workstream reads.
type Config struct {
	IaC      []Entry `yaml:"iac"`
	Conftest struct {
		Policy []string `yaml:"policy"`
	} `yaml:"conftest"`
	Tags []string `yaml:"tags"`
}

// Load decodes the running pack's settings. Keys of other workstreams are
// ignored here (each validates its own).
func Load(s packs.Settings) Config {
	var c Config
	if s.Raw.Kind != yaml.MappingNode {
		return c
	}
	for i := 0; i+1 < len(s.Raw.Content); i += 2 {
		k, v := s.Raw.Content[i].Value, s.Raw.Content[i+1]
		switch k {
		case "iac":
			_ = v.Decode(&c.IaC)
		case "conftest":
			_ = v.Decode(&c.Conftest)
		case "tags":
			_ = v.Decode(&c.Tags)
		}
	}
	for i := range c.IaC {
		c.IaC[i].Path = CleanPath(c.IaC[i].Path)
		c.IaC[i].Kind = strings.ToLower(strings.TrimSpace(c.IaC[i].Kind))
	}
	return c
}

// CleanPath is a repo-relative slash path without ./ or a trailing slash.
func CleanPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	if p == "" {
		return ""
	}
	p = path.Clean(p)
	if p == "." {
		return "."
	}
	return strings.TrimPrefix(p, "./")
}

// Under reports whether the slash path p is under the IaC path dir.
func Under(dir, p string) bool {
	p = CleanPath(p)
	if dir == "." || dir == "" {
		return true
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}

// Entries returns the entries whose kind is one of kinds (all with none).
func (c Config) Entries(kinds ...string) []Entry {
	var out []Entry
	for _, e := range c.IaC {
		if len(kinds) == 0 || contains(kinds, e.Kind) {
			out = append(out, e)
		}
	}
	return out
}

// Changed reports whether a changed (not deleted) path is under one of the
// entries of the given kinds.
func (c Config) Changed(in *packs.Input, kinds ...string) bool {
	return len(c.ChangedEntries(in, kinds...)) > 0
}

// ChangedEntries returns the entries of the given kinds with a changed path
// under them; with --all, every entry of those kinds.
func (c Config) ChangedEntries(in *packs.Input, kinds ...string) []Entry {
	var out []Entry
	for _, e := range c.Entries(kinds...) {
		if in.All {
			out = append(out, e)
			continue
		}
		for _, p := range in.ChangedPaths() {
			if Under(e.Path, p) {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// --- validators (packs.RegisterPack ConfigKeys) -------------------------

func at(n yaml.Node, format string, a ...any) error {
	return fmt.Errorf("line %d: %s", n.Line, fmt.Sprintf(format, a...))
}

func checkRelPath(n yaml.Node, what string) error {
	if n.Kind != yaml.ScalarNode || strings.TrimSpace(n.Value) == "" {
		return at(n, "%s must be a non-empty path", what)
	}
	p := strings.ReplaceAll(n.Value, `\`, "/")
	if strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':') {
		return at(n, "%s %q must be relative to the repository", what, n.Value)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return at(n, "%s %q must not contain ..", what, n.Value)
		}
	}
	return nil
}

// ValidateIaC validates packs.cloud.iac: a list of {path, kind}.
func ValidateIaC(n yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return at(n, "iac must be a list of {path, kind}")
	}
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			return at(*item, "iac entries must be {path, kind}")
		}
		var hasPath, hasKind bool
		for i := 0; i+1 < len(item.Content); i += 2 {
			k, v := item.Content[i], item.Content[i+1]
			switch k.Value {
			case "path":
				hasPath = true
				if err := checkRelPath(*v, "iac path"); err != nil {
					return err
				}
			case "kind":
				hasKind = true
				if v.Kind != yaml.ScalarNode || !contains(Kinds, strings.ToLower(v.Value)) {
					return at(*v, "iac kind %q is not one of %s", v.Value, strings.Join(Kinds, ", "))
				}
			default:
				return at(*k, "unknown iac key %q (path, kind)", k.Value)
			}
		}
		if !hasPath || !hasKind {
			return at(*item, "an iac entry needs both path and kind")
		}
	}
	return nil
}

// ValidateConftest validates packs.cloud.conftest: {policy: [paths]}.
func ValidateConftest(n yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return at(n, "conftest must be a mapping {policy: [paths]}")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Value != "policy" {
			return at(*k, "unknown conftest key %q (policy)", k.Value)
		}
		// A list only: Load decodes a list, and the Union merge of
		// conftest.policy works on lists, so a scalar would be dropped.
		if v.Kind != yaml.SequenceNode {
			return at(*v, "conftest policy must be a list of paths (policy: [%s])", v.Value)
		}
		for _, it := range v.Content {
			if err := checkRelPath(*it, "conftest policy"); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateTags validates packs.cloud.tags: a list of tag names.
func ValidateTags(n yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return at(n, "tags must be a list of tag names")
	}
	for _, it := range n.Content {
		if it.Kind != yaml.ScalarNode || strings.TrimSpace(it.Value) == "" {
			return at(*it, "tags must be non-empty strings")
		}
	}
	return nil
}
