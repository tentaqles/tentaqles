package packs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"gopkg.in/yaml.v3"
)

// Settings is one pack's merged config (manifest + tq-packs.yaml).
type Settings struct {
	Name      string
	Enabled   bool
	Tools     []string
	Timeouts  map[string]time.Duration
	ToolPaths map[string]string // trusted layer only
	Baseline  string
	// Raw is the merged packs.<name> mapping node. Decode it with Decode.
	Raw yaml.Node
}

// Declared reports whether tool is in the pack's tools: list.
func (s Settings) Declared(tool string) bool { return contains(s.Tools, tool) }

// Decode decodes the merged pack node into v (a struct with yaml tags).
func (s Settings) Decode(v any) error {
	if s.Raw.Kind == 0 {
		return nil
	}
	return s.Raw.Decode(v)
}

// Node returns the merged node at a dotted path ("contracts.paths"), or nil.
func (s Settings) Node(path string) *yaml.Node {
	n := &s.Raw
	for _, k := range strings.Split(path, ".") {
		if n.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == k {
				next = n.Content[i+1]
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}

// Strings returns the string list at a dotted path.
func (s Settings) Strings(path string) []string {
	n := s.Node(path)
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode && n.Value != "" {
		return []string{n.Value}
	}
	var out []string
	for _, c := range n.Content {
		if c.Kind == yaml.ScalarNode {
			out = append(out, c.Value)
		}
	}
	return out
}

// String returns the scalar at a dotted path, or "".
func (s Settings) String(path string) string {
	if n := s.Node(path); n != nil && n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return ""
}

// Problem is a tq-packs.yaml value the merge ignored (it would loosen the
// trusted layer) or could not read.
type Problem struct {
	Pack, Key, Message string
}

func (p Problem) String() string {
	if p.Key == "" {
		return p.Message
	}
	return fmt.Sprintf("packs.%s.%s: %s", p.Pack, p.Key, p.Message)
}

// Config is the merged pack config of a repository.
type Config struct {
	Packs map[string]Settings
	// Problems lists what the merge ignored from tq-packs.yaml.
	Problems []Problem
	// RepoFile is the tq-packs.yaml path that was merged, "" when none.
	RepoFile string
}

// Pack returns a pack's settings (zero when absent).
func (c Config) Pack(name string) Settings {
	if s, ok := c.Packs[name]; ok {
		return s
	}
	return Settings{Name: name}
}

// IsEnabled reports whether pack is enabled.
func (c Config) IsEnabled(name string) bool { return c.Pack(name).Enabled }

// Enabled returns the enabled pack names, sorted.
func (c Config) Enabled() []string {
	var out []string
	for n, s := range c.Packs {
		if s.Enabled {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Canonical is the merged config as plain data (what the hash covers and
// what `tq pack list --json` prints).
func (c Config) Canonical() map[string]any {
	out := map[string]any{}
	for _, n := range Names() {
		s, ok := c.Packs[n]
		if !ok {
			continue
		}
		var raw any
		if s.Raw.Kind != 0 {
			raw = nodeValue(&s.Raw)
		}
		m, _ := raw.(map[string]any)
		if m == nil {
			m = map[string]any{}
		}
		m["enabled"] = s.Enabled
		tools := append([]string{}, s.Tools...)
		sort.Strings(tools)
		m["tools"] = tools
		to := map[string]any{}
		for k, v := range s.Timeouts {
			to[k] = v.String()
		}
		m["timeouts"] = to
		tp := map[string]any{}
		for k, v := range s.ToolPaths {
			tp[k] = v
		}
		m["tool_paths"] = tp
		m["baseline"] = s.Baseline
		out[n] = m
	}
	return out
}

// Hash is the config_hash: sha256 over the canonical JSON of the merged config.
func (c Config) Hash() string {
	b, _ := json.Marshal(c.Canonical()) // encoding/json sorts map keys
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// nodeValue turns a YAML node into plain data with sorted-key maps.
func nodeValue(n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			return nodeValue(n.Content[0])
		}
		return nil
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			m[n.Content[i].Value] = nodeValue(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, nodeValue(c))
		}
		return out
	case yaml.AliasNode:
		if n.Alias != nil {
			return nodeValue(n.Alias)
		}
		return nil
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return n.Value
	}
	return v
}

// ErrRepoFile wraps a tq-packs.yaml that does not parse or validate.
var ErrRepoFile = errors.New("tq-packs.yaml is invalid")

// LoadConfig merges the manifest's packs: block (m may be nil outside a
// trusted workspace) with repoRoot/tq-packs.yaml when present. An invalid
// repo file returns the manifest-only config and an error wrapping
// ErrRepoFile; callers that must fail closed (tq pack check) treat it as a
// config error, the hook uses the manifest-only config.
func LoadConfig(m *manifest.Manifest, repoRoot string) (Config, error) {
	var trusted map[string]manifest.PackConfig
	if m != nil {
		trusted = m.Packs
	}
	var repo map[string]manifest.PackConfig
	var rerr error
	file := ""
	if repoRoot != "" {
		p := filepath.Join(repoRoot, manifest.RepoPacksFile)
		if raw, err := readCapped(p, 1<<20); err == nil {
			file = p
			repo, rerr = manifest.ParseRepoPacks(raw)
			if rerr != nil {
				rerr = fmt.Errorf("%w: %v", ErrRepoFile, rerr)
				repo = nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			rerr = fmt.Errorf("%w: %v", ErrRepoFile, err)
		}
	}
	c := Merge(trusted, repo)
	if repo != nil {
		c.RepoFile = file
	}
	return c, rerr
}

func readCapped(p string, max int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", p, max)
	}
	buf := make([]byte, fi.Size())
	n, err := f.Read(buf)
	if err != nil && n == 0 && fi.Size() > 0 {
		return nil, err
	}
	return buf[:n], nil
}

// Merge applies the add-only rules of spec C §2.3: the repo layer may enable
// a pack, add tools and path lists and tighten bounds, never loosen a trusted
// value. Ignored repo values are listed in Problems.
func Merge(trusted, repo map[string]manifest.PackConfig) Config {
	c := Config{Packs: map[string]Settings{}}
	for _, name := range Names() {
		t, tok := trusted[name]
		r, rok := repo[name]
		if !tok && !rok {
			continue
		}
		s := Settings{Name: name, Timeouts: map[string]time.Duration{}, ToolPaths: map[string]string{}}
		s.Enabled = (tok && t.IsEnabled()) || (rok && r.IsEnabled())
		if tok && rok && t.Enabled != nil && *t.Enabled && r.Enabled != nil && !*r.Enabled {
			c.Problems = append(c.Problems, Problem{name, "enabled", "the repo file cannot disable a pack the manifest enabled (ignored)"})
		}
		s.Tools = union(t.Tools, r.Tools)
		for k, v := range t.Timeouts {
			s.Timeouts[k] = time.Duration(v)
		}
		for k, v := range r.Timeouts {
			rv := time.Duration(v)
			if cur, ok := s.Timeouts[k]; !ok || rv < cur {
				s.Timeouts[k] = rv
			} else if rv > cur {
				c.Problems = append(c.Problems, Problem{name, "timeouts." + k, "the repo file cannot raise a timeout (ignored)"})
			}
		}
		for k, v := range t.ToolPaths {
			s.ToolPaths[k] = v
		}
		if len(r.ToolPaths) > 0 {
			c.Problems = append(c.Problems, Problem{name, "tool_paths", "tool_paths is trusted-layer only; the repo file's value is ignored"})
		}
		s.Baseline = t.Baseline
		if s.Baseline == "" {
			s.Baseline = r.Baseline
		} else if r.Baseline != "" && r.Baseline != t.Baseline {
			c.Problems = append(c.Problems, Problem{name, "baseline", "the repo file cannot move the manifest's baseline (ignored)"})
		}
		kinds := mergeKinds(name)
		var tn, rn *yaml.Node
		if tok {
			tn = &t.Raw
		}
		if rok {
			rn = &r.Raw
		}
		merged := mergeNode(tn, rn, "", kinds, func(key, msg string) {
			c.Problems = append(c.Problems, Problem{name, key, msg})
		})
		// Framework keys live in the typed fields only, already merged.
		var keep []*yaml.Node
		for i := 0; i+1 < len(merged.Content); i += 2 {
			if !contains(manifest.FrameworkPackKeys, merged.Content[i].Value) {
				keep = append(keep, merged.Content[i], merged.Content[i+1])
			}
		}
		merged.Content = keep
		if merged.Kind != yaml.MappingNode {
			merged = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		s.Raw = *merged
		c.Packs[name] = s
	}
	return c
}

func union(a, b []string) []string {
	var out []string
	for _, x := range append(append([]string{}, a...), b...) {
		if !contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// kindFor finds the merge kind of a dotted key; * matches one segment.
func kindFor(kinds map[string]MergeKind, key string) MergeKind {
	if k, ok := kinds[key]; ok {
		return k
	}
	segs := strings.Split(key, ".")
	for pat, k := range kinds {
		ps := strings.Split(pat, ".")
		if len(ps) != len(segs) {
			continue
		}
		ok := true
		for i := range ps {
			if ps[i] != "*" && ps[i] != segs[i] {
				ok = false
				break
			}
		}
		if ok {
			return k
		}
	}
	return MergeTrusted
}

// mergeNode merges two pack nodes (either may be nil). Framework keys are
// merged by Merge itself and copied from the trusted side here.
func mergeNode(t, r *yaml.Node, prefix string, kinds map[string]MergeKind, problem func(key, msg string)) *yaml.Node {
	switch {
	case t == nil && r == nil:
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	case t == nil || t.Kind == 0 || isNull(t):
		if r == nil || isNull(r) {
			return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		return stripRepoOnly(cloneNode(r), prefix, kinds, problem)
	case r == nil || r.Kind == 0 || isNull(r):
		return cloneNode(t)
	}
	if t.Kind != yaml.MappingNode || r.Kind != yaml.MappingNode {
		return cloneNode(t)
	}
	out := cloneNode(t)
	for i := 0; i+1 < len(r.Content); i += 2 {
		k := r.Content[i].Value
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if prefix == "" && contains(manifest.FrameworkPackKeys, k) {
			continue
		}
		rv := r.Content[i+1]
		idx := -1
		for j := 0; j+1 < len(out.Content); j += 2 {
			if out.Content[j].Value == k {
				idx = j + 1
			}
		}
		kind := kindFor(kinds, key)
		if idx < 0 {
			if kind == MergeIgnoreRepo {
				problem(key, "trusted-layer only; the repo file's value is ignored")
				continue
			}
			out.Content = append(out.Content, cloneNode(r.Content[i]), stripRepoOnly(cloneNode(rv), key, kinds, problem))
			continue
		}
		tv := out.Content[idx]
		switch kind {
		case MergeUnion:
			out.Content[idx] = unionNode(tv, rv)
		case MergeMin, MergeMax:
			a, aok := numeric(tv.Value)
			b, bok := numeric(rv.Value)
			switch {
			case !aok || !bok:
				problem(key, "not a comparable value; the manifest's value is kept")
			case (kind == MergeMin && b < a) || (kind == MergeMax && b > a):
				out.Content[idx] = cloneNode(rv)
			case b != a:
				problem(key, "the repo file cannot loosen this bound (ignored)")
			}
		case MergeOr:
			if rv.Value == "true" {
				out.Content[idx] = cloneNode(rv)
			} else if tv.Value == "true" {
				problem(key, "the repo file cannot switch this off (ignored)")
			}
		case MergeRepo:
			out.Content[idx] = cloneNode(rv)
		case MergeIgnoreRepo:
			problem(key, "trusted-layer only; the repo file's value is ignored")
		default:
			if tv.Kind == yaml.MappingNode && rv.Kind == yaml.MappingNode {
				out.Content[idx] = mergeNode(tv, rv, key, kinds, problem)
			} else if !sameNode(tv, rv) {
				problem(key, "set by the manifest; the repo file cannot change it (ignored)")
			}
		}
	}
	return out
}

// stripRepoOnly drops trusted-layer-only keys from a node the repo adds.
func stripRepoOnly(n *yaml.Node, prefix string, kinds map[string]MergeKind, problem func(key, msg string)) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return n
	}
	var keep []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if kindFor(kinds, key) == MergeIgnoreRepo {
			problem(key, "trusted-layer only; the repo file's value is ignored")
			continue
		}
		keep = append(keep, n.Content[i], stripRepoOnly(n.Content[i+1], key, kinds, problem))
	}
	n.Content = keep
	return n
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || (n.Value == "" && n.Tag == ""))
}

func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, x := range n.Content {
		c.Content[i] = cloneNode(x)
	}
	return &c
}

func sameNode(a, b *yaml.Node) bool {
	x, _ := json.Marshal(nodeValue(a))
	y, _ := json.Marshal(nodeValue(b))
	return string(x) == string(y)
}

func unionNode(a, b *yaml.Node) *yaml.Node {
	out := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Line: a.Line, Column: a.Column}
	seen := map[string]bool{}
	add := func(n *yaml.Node) {
		items := n.Content
		if n.Kind == yaml.ScalarNode {
			items = []*yaml.Node{n}
		}
		for _, it := range items {
			k, _ := json.Marshal(nodeValue(it))
			if !seen[string(k)] {
				seen[string(k)] = true
				out.Content = append(out.Content, cloneNode(it))
			}
		}
	}
	add(a)
	add(b)
	return out
}

var sizeRe = regexp.MustCompile(`^(?i)\s*([0-9]+(?:\.[0-9]+)?)\s*([kmgtp]i?b|b)?\s*$`)

// numeric reads a bound: a number, a duration (600s, 7d) or a byte size
// (10GB, 512MiB). Units of one kind compare correctly; mixed kinds are a
// configuration mistake either way.
func numeric(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	if m := sizeRe.FindStringSubmatch(s); m != nil && m[2] != "" {
		f, _ := strconv.ParseFloat(m[1], 64)
		u := strings.ToLower(m[2])
		bin := strings.Contains(u, "i")
		base := 1000.0
		if bin {
			base = 1024
		}
		pow := strings.IndexByte("bkmgtp", u[0])
		for i := 0; i < pow; i++ {
			f *= base
		}
		return f, true
	}
	if d, err := manifest.ParseDuration(s); err == nil {
		return float64(d), true
	}
	return 0, false
}
