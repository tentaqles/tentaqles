package packs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
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
	Baseline  string            // repo-relative slash path, cleaned
	// Raw is the merged packs.<name> mapping node (the pack's own keys only).
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
// Use NodeAt for keys that contain a dot.
func (s Settings) Node(dotted string) *yaml.Node { return s.NodeAt(strings.Split(dotted, ".")...) }

// NodeAt returns the merged node at a key path, or nil.
func (s Settings) NodeAt(keys ...string) *yaml.Node {
	n := &s.Raw
	for _, k := range keys {
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

// Strings returns the string list at a dotted path (a scalar is a one-item
// list; a mapping returns nil).
func (s Settings) Strings(dotted string) []string {
	n := s.Node(dotted)
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value != "" && n.Tag != "!!null" {
			return []string{n.Value}
		}
	case yaml.SequenceNode:
		var out []string
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode {
				out = append(out, c.Value)
			}
		}
		return out
	}
	return nil
}

// String returns the scalar at a dotted path, or "".
func (s Settings) String(dotted string) string {
	if n := s.Node(dotted); n != nil && n.Kind == yaml.ScalarNode {
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
		return "packs." + p.Pack + ": " + p.Message
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
	hash     string
	hashErr  error
}

// Pack returns a pack's settings (zero when absent).
func (c Config) Pack(name string) Settings {
	if s, ok := c.Packs[name]; ok {
		return s
	}
	return Settings{Name: name}
}

// IsEnabled reports whether pack is enabled. Only the trusted manifest
// enables a pack; the repo layer never does.
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

// Canonical is the merged config as plain data, for display (tq pack list).
// Scalars are their YAML text; non-finite numbers stay strings.
func (c Config) Canonical() map[string]any {
	out := map[string]any{}
	for _, n := range Names() {
		s, ok := c.Packs[n]
		if !ok {
			continue
		}
		m := map[string]any{}
		if s.Raw.Kind != 0 {
			if v, err := nodeValue(&s.Raw, 0, false); err == nil {
				if mm, ok := v.(map[string]any); ok {
					m = mm
				}
			}
		}
		for k, v := range frameworkValues(s) {
			m[k] = v
		}
		out[n] = m
	}
	return out
}

func frameworkValues(s Settings) map[string]any {
	tools := append([]string{}, s.Tools...)
	sort.Strings(tools)
	to := map[string]any{}
	for k, v := range s.Timeouts {
		to[k] = v.String()
	}
	tp := map[string]any{}
	for k, v := range s.ToolPaths {
		tp[k] = v
	}
	return map[string]any{"enabled": s.Enabled, "tools": tools, "timeouts": to, "tool_paths": tp, "baseline": s.Baseline}
}

// Hash is the config_hash: sha256 over a canonical JSON where every scalar
// is its resolved YAML tag plus its text, so no value can make the encoding
// fail or collapse. It is "" when Err() is set.
func (c Config) Hash() string { return c.hash }

// Err reports a config that cannot be hashed (too deep, a cycle): treat it
// as invalid (exit 2) and fail closed.
func (c Config) Err() error { return c.hashErr }

func (c *Config) computeHash() {
	doc := map[string]any{}
	for _, n := range Names() {
		s, ok := c.Packs[n]
		if !ok {
			continue
		}
		var raw any = map[string]any{}
		if s.Raw.Kind != 0 {
			v, err := nodeValue(&s.Raw, 0, true)
			if err != nil {
				c.hash, c.hashErr = "", fmt.Errorf("packs.%s: %w", n, err)
				return
			}
			raw = v
		}
		doc[n] = map[string]any{"framework": frameworkValues(s), "pack": raw}
	}
	b, err := json.Marshal(doc)
	if err != nil || len(b) == 0 {
		c.hash, c.hashErr = "", fmt.Errorf("packs: config cannot be hashed: %v", err)
		return
	}
	sum := sha256.Sum256(b)
	c.hash = "sha256:" + hex.EncodeToString(sum[:])
}

const maxNodeDepth = 64

// nodeValue turns a YAML node into plain data. tagged: scalars become
// [tag, text] pairs (the hash form); otherwise text (finite numbers and
// booleans decoded for display).
func nodeValue(n *yaml.Node, depth int, tagged bool) (any, error) {
	if depth > maxNodeDepth {
		return nil, errors.New("nesting too deep (or an alias cycle)")
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) > 0 {
			return nodeValue(n.Content[0], depth+1, tagged)
		}
		return nil, nil
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			v, err := nodeValue(n.Content[i+1], depth+1, tagged)
			if err != nil {
				return nil, err
			}
			m[n.Content[i].Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := nodeValue(c, depth+1, tagged)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.AliasNode:
		if n.Alias == nil {
			return nil, nil
		}
		return nodeValue(n.Alias, depth+1, tagged)
	}
	if tagged {
		return []string{n.ShortTag(), n.Value}, nil
	}
	switch n.ShortTag() {
	case "!!int", "!!float":
		if f, err := strconv.ParseFloat(strings.ReplaceAll(n.Value, "_", ""), 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			return f, nil
		}
		return n.Value, nil
	case "!!bool":
		var b bool
		if n.Decode(&b) == nil {
			return b, nil
		}
	case "!!null":
		return nil, nil
	}
	return n.Value, nil
}

// ErrRepoFile wraps a tq-packs.yaml that does not parse or validate.
var ErrRepoFile = errors.New("tq-packs.yaml is invalid")

// LoadConfig merges the manifest's packs: block (m may be nil outside a
// trusted workspace, and then the repo layer is not read: no pack is
// enabled) with repoRoot/tq-packs.yaml when present. An invalid repo file
// returns the manifest-only config and an error wrapping ErrRepoFile;
// callers that must fail closed (tq pack check) treat it as a config error.
// A config that cannot be hashed is an error too (Config.Err).
func LoadConfig(m *manifest.Manifest, repoRoot string) (Config, error) {
	if m == nil {
		return Merge(nil, nil), nil
	}
	var repo map[string]manifest.PackConfig
	var rerr error
	file := ""
	if repoRoot != "" {
		p := filepath.Join(repoRoot, manifest.RepoPacksFile)
		if raw, err := readRegular(p, 1<<20); err == nil {
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
	c := Merge(m.Packs, repo)
	if repo != nil {
		c.RepoFile = file
	}
	if rerr == nil && c.Err() != nil {
		rerr = c.Err()
	}
	return c, rerr
}

// readRegular reads a regular file (never a FIFO or a device, which could
// block the caller), at most max bytes.
func readRegular(p string, max int64) ([]byte, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", p)
	}
	return readCapped(p, max)
}

func readCapped(p string, max int64) ([]byte, error) {
	f, err := openNoBlock(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", p)
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", p, max)
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", p, max)
	}
	return b, nil
}

// normRepoPath is a repo-relative slash path without ./ (baselines).
func normRepoPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	if p == "" {
		return ""
	}
	return strings.TrimPrefix(path.Clean(p), "./")
}

// Merge applies the add-only rules of spec C §2.3 as ruled at review: only
// the trusted manifest enables a pack (a repo block for a pack the manifest
// does not configure is ignored); the repo may add tools and path lists and
// tighten bounds, never loosen a trusted value or a pack default. Every
// pack key without a registered rule is trusted-layer only. Ignored repo
// values are listed in Problems.
func Merge(trusted, repo map[string]manifest.PackConfig) Config {
	c := Config{Packs: map[string]Settings{}}
	problem := func(pack, key, msg string) { c.Problems = append(c.Problems, Problem{pack, key, msg}) }
	for _, name := range Names() {
		t, tok := trusted[name]
		r, rok := repo[name]
		if !tok {
			if rok {
				problem(name, "", "the repo file cannot enable or configure a pack the manifest does not configure (ignored)")
			}
			continue
		}
		s := Settings{Name: name, Enabled: t.IsEnabled(), Timeouts: map[string]time.Duration{}, ToolPaths: map[string]string{}}
		s.Tools = append([]string{}, t.Tools...)
		for k, v := range t.Timeouts {
			s.Timeouts[k] = time.Duration(v)
		}
		for k, v := range t.ToolPaths {
			s.ToolPaths[k] = v
		}
		s.Baseline = normRepoPath(t.Baseline)
		if rok {
			if r.Enabled != nil && *r.Enabled != s.Enabled {
				problem(name, "enabled", "only the manifest enables or disables a pack (ignored)")
			}
			s.Tools = union(s.Tools, r.Tools)
			for k, v := range r.Timeouts {
				rv := time.Duration(v)
				limit, ok := s.Timeouts[k]
				if !ok {
					limit = defaultToolTimeout(name, k)
				}
				if rv <= limit {
					s.Timeouts[k] = rv
				} else {
					problem(name, "timeouts."+k, "the repo file cannot raise a timeout above "+limit.String()+" (ignored)")
				}
			}
			if len(r.ToolPaths) > 0 {
				problem(name, "tool_paths", "tool_paths is trusted-layer only; the repo file's value is ignored")
			}
			if r.Baseline != "" && normRepoPath(r.Baseline) != s.Baseline {
				problem(name, "baseline", "the baseline is trusted-layer only (ignored)")
			}
		}
		rules := mergeRules(name)
		merged := cloneNode(&t.Raw)
		if merged == nil || merged.Kind != yaml.MappingNode {
			merged = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		if rok && r.Raw.Kind == yaml.MappingNode {
			merged = mergeMapping(merged, &r.Raw, nil, rules, nil, func(key, msg string) { problem(name, key, msg) })
		}
		var keep []*yaml.Node
		for i := 0; i+1 < len(merged.Content); i += 2 {
			if !contains(manifest.FrameworkPackKeys, merged.Content[i].Value) {
				keep = append(keep, merged.Content[i], merged.Content[i+1])
			}
		}
		merged.Content = keep
		s.Raw = *merged
		c.Packs[name] = s
	}
	c.computeHash()
	return c
}

// defaultToolTimeout is the smallest default of the pack's checks using tool
// (DefaultToolTimeout when they set none): a repo timeout may only be lower.
func defaultToolTimeout(pack, tool string) time.Duration {
	limit := time.Duration(0)
	for _, ch := range Checks(pack) {
		if ch.Kind != External || ch.Tool != tool {
			continue
		}
		d := ch.Timeout
		if d <= 0 {
			d = DefaultToolTimeout
		}
		if limit == 0 || d < limit {
			limit = d
		}
	}
	if limit == 0 {
		limit = DefaultToolTimeout
	}
	return limit
}

// DefaultToolTimeout is the tool timeout of a check that sets none.
const DefaultToolTimeout = 120 * time.Second

func union(a, b []string) []string {
	var out []string
	for _, x := range append(append([]string{}, a...), b...) {
		if !contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// ruleFor finds the rule of a key path: the exact pattern first, then the
// one with the fewest wildcards, then the lexically smallest (RegisterPack
// refuses overlapping patterns with different rules, so ties agree).
func ruleFor(rules map[string]MergeRule, keys []string) (MergeRule, bool) {
	best, bestW, found := "", 0, false
	for pat := range rules {
		ps := strings.Split(pat, ".")
		if len(ps) != len(keys) {
			continue
		}
		w := 0
		ok := true
		for i := range ps {
			if ps[i] == "*" {
				w++
			} else if ps[i] != keys[i] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if !found || w < bestW || (w == bestW && pat < best) {
			best, bestW, found = pat, w, true
		}
	}
	if !found {
		return MergeRule{}, false
	}
	return rules[best], true
}

// hasRuleBelow reports whether some rule addresses a key under prefix.
func hasRuleBelow(rules map[string]MergeRule, keys []string) bool {
	for pat := range rules {
		ps := strings.Split(pat, ".")
		if len(ps) <= len(keys) {
			continue
		}
		ok := true
		for i := range keys {
			if ps[i] != "*" && ps[i] != keys[i] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// mergeMapping merges the repo mapping r into the trusted mapping t (t may
// be an empty mapping when the manifest is silent). parent is the explicit
// rule of the mapping itself, if any: a key with no rule of its own under a
// TrustedAdd or RepoValue parent inherits it; every other key without a
// rule is trusted-layer only. A key whose subtree has rules is always merged
// child by child, never cloned over its children's rules.
func mergeMapping(t, r *yaml.Node, prefix []string, rules map[string]MergeRule, parent *MergeRule, problem func(key, msg string)) *yaml.Node {
	out := cloneNode(t)
	for i := 0; i+1 < len(r.Content); i += 2 {
		k := r.Content[i].Value
		keys := append(append([]string(nil), prefix...), k)
		key := strings.Join(keys, ".")
		if len(prefix) == 0 && contains(manifest.FrameworkPackKeys, k) {
			continue
		}
		rv := r.Content[i+1]
		idx := -1
		for j := 0; j+1 < len(out.Content); j += 2 {
			if out.Content[j].Value == k {
				idx = j + 1
			}
		}
		var tv *yaml.Node
		if idx >= 0 {
			tv = out.Content[idx]
		}
		rule, explicit := ruleFor(rules, keys)
		if !explicit && parent != nil && (parent.Kind == MergeTrusted || parent.Kind == MergeRepo) {
			rule, explicit = *parent, true
		}
		if rv.Kind == yaml.MappingNode && (tv == nil || tv.Kind == yaml.MappingNode) && hasRuleBelow(rules, keys) {
			// The parent's own rule only decides whether the key may be added.
			if explicit {
				switch rule.Kind {
				case MergeTrusted, MergeRepo:
				default:
					if tv == nil {
						problem(key, "trusted-layer only; the repo file cannot add it (ignored)")
						continue
					}
				}
			}
			base := tv
			if base == nil {
				base = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: rv.Line, Column: rv.Column}
			}
			var inherit *MergeRule
			if explicit {
				r := rule
				inherit = &r
			}
			m := mergeMapping(base, rv, keys, rules, inherit, problem)
			if tv == nil && len(m.Content) == 0 {
				continue
			}
			if idx >= 0 {
				out.Content[idx] = m
			} else {
				out.Content = append(out.Content, cloneNode(r.Content[i]), m)
			}
			continue
		}
		val, ok, msg := mergeValue(rule, tv, rv)
		if msg != "" {
			problem(key, msg)
		}
		if !ok {
			continue
		}
		if idx >= 0 {
			out.Content[idx] = val
		} else {
			out.Content = append(out.Content, cloneNode(r.Content[i]), val)
		}
	}
	return out
}

// valueKind is how a bound is spelled: a bare number, a duration with a
// unit, or a byte size with a unit.
type valueKind int

const (
	kindNone valueKind = iota
	kindNumber
	kindDuration
	kindBytes
)

// boundOf reads a bound and its kind. Bare numbers are kindNumber; a value
// with a size suffix is kindBytes; anything manifest.ParseDuration reads
// with a unit is kindDuration.
func boundOf(s string) (float64, valueKind, bool) {
	s = strings.TrimSpace(s)
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return 0, kindNone, false
		}
		return f, kindNumber, true
	}
	if m := sizeRe.FindStringSubmatch(s); m != nil && m[2] != "" {
		f, _ := numeric(s)
		return f, kindBytes, true
	}
	if d, err := manifest.ParseDuration(s); err == nil {
		return float64(d), kindDuration, true
	}
	return 0, kindNone, false
}

// compareBound reads rv in the unit of ref. A duration ref reads both sides
// with manifest.ParseDuration (a bare number is seconds there, as the packs
// decode it); otherwise the kinds must match.
func compareBound(ref, rv string) (a, b float64, err string) {
	av, ak, aok := boundOf(ref)
	if !aok {
		return 0, 0, "the trusted value is not comparable; it is kept"
	}
	if ak == kindDuration {
		ad, e1 := manifest.ParseDuration(ref)
		bd, e2 := manifest.ParseDuration(rv)
		if e1 != nil || e2 != nil {
			return 0, 0, "not a duration (ignored)"
		}
		return float64(ad), float64(bd), ""
	}
	bv, bk, bok := boundOf(rv)
	if !bok || bk != ak {
		return 0, 0, "not the same kind of value as the trusted one (a number, a duration or a size) (ignored)"
	}
	return av, bv, ""
}

// mergeValue applies one rule. tv is nil when the manifest is silent, and
// then bounds compare against the rule's pack default; a blank (null)
// trusted value means the default too, and a non-scalar trusted value is
// kept. It returns the value to keep, whether to set it, and a problem.
func mergeValue(rule MergeRule, tv, rv *yaml.Node) (*yaml.Node, bool, string) {
	same := tv != nil && sameNode(tv, rv)
	if same {
		return tv, false, ""
	}
	isMap := rv.Kind == yaml.MappingNode || (tv != nil && tv.Kind == yaml.MappingNode)
	// ref is the value the repo is compared against.
	ref, refOK := rule.Default, true
	if tv != nil {
		switch {
		case isNull(tv):
			ref = rule.Default
		case tv.Kind == yaml.ScalarNode:
			ref = tv.Value
		default:
			refOK = false
		}
	}
	switch rule.Kind {
	case MergeIgnoreRepo:
		return nil, false, "trusted-layer only; the repo file cannot set or change it (ignored)"
	case MergeTrusted:
		if tv == nil || isNull(tv) {
			return cloneNode(rv), true, ""
		}
		return nil, false, "set by the manifest; the repo file cannot change it (ignored)"
	case MergeRepo:
		return cloneNode(rv), true, ""
	case MergeUnion:
		if isMap {
			return nil, false, "a union applies to lists, not mappings (ignored)"
		}
		if tv == nil || isNull(tv) {
			return unionNode(&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}, rv), true, ""
		}
		return unionNode(tv, rv), true, ""
	case MergeOr:
		if rv.Kind != yaml.ScalarNode || (rv.Value != "true" && rv.Value != "false") {
			return nil, false, "not a boolean (ignored)"
		}
		if rv.Value == "true" {
			return cloneNode(rv), true, ""
		}
		return nil, false, "the repo file cannot switch this off (ignored)"
	case MergeMin, MergeMax:
		if !refOK {
			return nil, false, "the trusted value is not a scalar; it is kept"
		}
		if isMap || rv.Kind != yaml.ScalarNode {
			return nil, false, "not a comparable value (ignored)"
		}
		if rv0, _, ok := boundOf(rv.Value); ok {
			if rule.ZeroUnlimited && rv0 <= 0 {
				return nil, false, "0 or a negative value means no limit here; the repo file cannot remove a limit (ignored)"
			}
			if rv0 < 0 {
				return nil, false, "a negative bound (ignored)"
			}
		}
		if ref == "" {
			return nil, false, "the rule has no default (ignored)"
		}
		if a0, _, ok := boundOf(ref); ok && a0 <= 0 && rule.ZeroUnlimited && rule.Kind == MergeMin {
			// The trusted value is "no limit": any limit of the default's kind tightens.
			if _, _, msg := compareBound(rule.Default, rv.Value); msg != "" {
				return nil, false, msg
			}
			return cloneNode(rv), true, ""
		}
		a, b, msg := compareBound(ref, rv.Value)
		if msg != "" {
			return nil, false, msg
		}
		if b < 0 {
			return nil, false, "a negative bound (ignored)"
		}
		if rule.ZeroUnlimited && a <= 0 {
			a = math.Inf(1) // the trusted value is "no limit": any limit tightens
			if rule.Kind == MergeMax {
				a = 0
			}
		}
		if (rule.Kind == MergeMin && b <= a) || (rule.Kind == MergeMax && b >= a) {
			return cloneNode(rv), true, ""
		}
		return nil, false, "the repo file cannot loosen this bound (ignored)"
	case MergeOrdered:
		if !refOK {
			return nil, false, "the trusted value is not a scalar; it is kept"
		}
		if rv.Kind != yaml.ScalarNode {
			return nil, false, "not one of " + strings.Join(rule.Order, ", ") + " (ignored)"
		}
		ri := indexOf(rule.Order, rv.Value)
		if ri < 0 {
			return nil, false, "not one of " + strings.Join(rule.Order, ", ") + " (ignored)"
		}
		ti := indexOf(rule.Order, ref)
		if ti < 0 {
			return nil, false, "the trusted value is not in the order; it is kept"
		}
		if ri >= ti {
			return cloneNode(rv), true, ""
		}
		return nil, false, "the repo file cannot loosen this value (ignored)"
	}
	return nil, false, "unknown merge rule (ignored)"
}

// isNull reports a blank or ~ scalar.
func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || (n.Value == "" && n.Style == 0))
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
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
	x, errA := nodeValue(a, 0, true)
	y, errB := nodeValue(b, 0, true)
	if errA != nil || errB != nil {
		return false
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya)
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
			v, err := nodeValue(it, 0, true)
			if err != nil {
				continue
			}
			k, _ := json.Marshal(v)
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

var sizeRe = regexp.MustCompile(`^(?i)\s*(-?[0-9]+(?:\.[0-9]+)?)\s*([kmgtp]i?b|b)?\s*$`)

// numeric reads a bound: a number, a duration (600s, 7d) or a byte size
// (10GB, 512MiB).
func numeric(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return 0, false
		}
		return f, true
	}
	if m := sizeRe.FindStringSubmatch(s); m != nil && m[2] != "" {
		f, _ := strconv.ParseFloat(m[1], 64)
		u := strings.ToLower(m[2])
		base := 1000.0
		if strings.Contains(u, "i") {
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
