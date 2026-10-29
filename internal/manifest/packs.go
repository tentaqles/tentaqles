package manifest

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// KnownPacks are the pack names a packs: block may use (spec C §2.2).
var KnownPacks = []string{"data", "ml", "cloud", "db"}

// MaxToolTimeout caps packs.<p>.timeouts.<tool>.
const MaxToolTimeout = 1800 * time.Second

// FrameworkPackKeys are the keys of packs.<p> the framework owns; every other
// key belongs to the pack and is checked by its registered validator.
var FrameworkPackKeys = []string{"enabled", "tools", "timeouts", "tool_paths", "baseline"}

var toolNameRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Duration is a time.Duration in YAML: "600s", "10m", "1h", or a bare number
// of seconds.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// ParseDuration reads "600s", "10m", "7d" (days) or a bare number of seconds.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n * float64(time.Second)), nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil {
			return 0, fmt.Errorf("bad duration %q", s)
		}
		return time.Duration(n * float64(24*time.Hour)), nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("bad duration %q (use 600s, 10m, 1h or 7d)", s)
	}
	return v, nil
}

// PackConfig is one packs.<name> block. Raw is the whole node, so a pack
// decodes its own settings with line numbers for its errors.
type PackConfig struct {
	Enabled   *bool               `yaml:"enabled,omitempty"`
	Tools     []string            `yaml:"tools,omitempty"`
	Timeouts  map[string]Duration `yaml:"timeouts,omitempty"`
	ToolPaths map[string]string   `yaml:"tool_paths,omitempty"`
	Baseline  string              `yaml:"baseline,omitempty"`
	Raw       yaml.Node           `yaml:"-"`
}

func (p *PackConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain PackConfig
	var v plain
	if n.Kind == yaml.MappingNode {
		if err := n.Decode(&v); err != nil {
			return err
		}
	} else if !(n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == "")) {
		return fmt.Errorf("line %d: a pack block must be a mapping", n.Line)
	}
	*p = PackConfig(v)
	p.Raw = *n
	return nil
}

// IsEnabled: enabled wins when set; otherwise a non-empty block enables the pack.
func (p PackConfig) IsEnabled() bool {
	if p.Enabled != nil {
		return *p.Enabled
	}
	return p.Raw.Kind == yaml.MappingNode && len(p.Raw.Content) > 0
}

var (
	packValidatorsMu sync.RWMutex
	packValidators   = map[string]func(yaml.Node) error{}
)

// RegisterPackValidator installs the validator of a pack's own keys. The
// packs framework calls it (through packs.RegisterPack); manifest never
// imports packs. A second registration for the same name replaces the first.
func RegisterPackValidator(name string, fn func(yaml.Node) error) {
	packValidatorsMu.Lock()
	defer packValidatorsMu.Unlock()
	packValidators[name] = fn
}

func packValidator(name string) func(yaml.Node) error {
	packValidatorsMu.RLock()
	defer packValidatorsMu.RUnlock()
	return packValidators[name]
}

// ValidatePacks checks a packs: map (the manifest block or tq-packs.yaml):
// known pack names, tool names in [a-z0-9_-], timeouts within the cap, a repo
// relative baseline, then the pack's registered validator. With no validator
// registered (a binary without that pack linked) the pack's keys are kept and
// not checked.
func ValidatePacks(packs map[string]PackConfig) error {
	names := make([]string, 0, len(packs))
	for n := range packs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		p := packs[name]
		if !contains(KnownPacks, name) {
			return fmt.Errorf("packs: unknown pack %q (known: %s)", name, strings.Join(KnownPacks, ", "))
		}
		for _, t := range p.Tools {
			if !toolNameRe.MatchString(t) {
				return fmt.Errorf("packs.%s.tools: %q: tool names use only a-z, 0-9, _ and -", name, t)
			}
		}
		for t, d := range p.Timeouts {
			if !toolNameRe.MatchString(t) {
				return fmt.Errorf("packs.%s.timeouts: %q: tool names use only a-z, 0-9, _ and -", name, t)
			}
			if d <= 0 || time.Duration(d) > MaxToolTimeout {
				return fmt.Errorf("packs.%s.timeouts.%s: must be between 1s and %s", name, t, MaxToolTimeout)
			}
		}
		for t, v := range p.ToolPaths {
			if !toolNameRe.MatchString(t) {
				return fmt.Errorf("packs.%s.tool_paths: %q: tool names use only a-z, 0-9, _ and -", name, t)
			}
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("packs.%s.tool_paths.%s: empty path", name, t)
			}
		}
		if b := p.Baseline; b != "" {
			c := path.Clean(strings.ReplaceAll(b, `\`, "/"))
			if path.IsAbs(c) || (len(c) > 1 && c[1] == ':') || c == ".." || strings.HasPrefix(c, "../") {
				return fmt.Errorf("packs.%s.baseline: %q must be a path inside the repository", name, b)
			}
		}
		if fn := packValidator(name); fn != nil && p.Raw.Kind == yaml.MappingNode {
			if err := fn(p.Raw); err != nil {
				return fmt.Errorf("packs.%s: %w", name, err)
			}
		}
	}
	return nil
}

// plainYAML refuses anchors, aliases, merge keys (<<) and deep nesting: the
// untrusted repo layer has no need for them, and they would bypass the
// per-key merge rules or recurse without end.
func plainYAML(n *yaml.Node, depth int) error {
	if depth > 64 {
		return fmt.Errorf("line %d: nested too deep", n.Line)
	}
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("line %d: aliases are not allowed in %s", n.Line, RepoPacksFile)
	}
	if n.Anchor != "" {
		return fmt.Errorf("line %d: anchors are not allowed in %s", n.Line, RepoPacksFile)
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "<<" || n.Content[i].Tag == "!!merge" {
				return fmt.Errorf("line %d: merge keys (<<) are not allowed in %s", n.Content[i].Line, RepoPacksFile)
			}
		}
	}
	for _, c := range n.Content {
		if err := plainYAML(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// RepoPacksFile is the add-only repo layer at a repository root (spec C §2.3).
const RepoPacksFile = "tq-packs.yaml"

// ParseRepoPacks reads tq-packs.yaml: the same schema as the manifest's
// packs: block, either at the top level or under a packs: key. It runs the
// manifest's secret scan and ValidatePacks.
func ParseRepoPacks(raw []byte) (map[string]PackConfig, error) {
	if loc := secretRe.FindIndex(raw); loc != nil {
		return nil, fmt.Errorf("%w (at byte %d of %s)", ErrSecretLike, loc[0], RepoPacksFile)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", RepoPacksFile, err)
	}
	out := map[string]PackConfig{}
	if len(doc.Content) == 0 {
		return out, nil
	}
	if err := plainYAML(&doc, 0); err != nil {
		return nil, fmt.Errorf("%s: %w", RepoPacksFile, err)
	}
	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: the top level must be a mapping of pack names", RepoPacksFile)
	}
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "packs" {
			top = top.Content[i+1]
			break
		}
	}
	if err := top.Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: %w", RepoPacksFile, err)
	}
	if err := ValidatePacks(out); err != nil {
		return nil, fmt.Errorf("%s: %w", RepoPacksFile, err)
	}
	return out, nil
}
