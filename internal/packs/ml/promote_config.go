package ml

// Config of the promotion gate, model cards and the drift check (spec C
// §4.1): the packs.ml keys models, promotion and drift. The keys paths,
// seed_helper and data_manifest belong to the lint workstream; this file only
// reads them.

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

// Model is one packs.ml.models entry.
type Model struct {
	Name    string      `yaml:"name"`
	Card    string      `yaml:"card"`
	Metrics string      `yaml:"metrics"`
	Paths   []string    `yaml:"paths"` // code of this model; empty = packs.ml.paths
	Drift   *DriftPaths `yaml:"drift"` // per-model override of the drift inputs
}

// DriftPaths are the per-model drift inputs.
type DriftPaths struct {
	Reference      string `yaml:"reference"`
	Current        string `yaml:"current"`
	CurrentCommand string `yaml:"current_command"`
}

// Promotion is packs.ml.promotion.
type Promotion struct {
	Protected []string `yaml:"protected"`
	Command   string   `yaml:"command"`
	ValidFor  string   `yaml:"valid_for"`
	Require   []string `yaml:"require"`
}

// Threshold is a per-feature drift override.
type Threshold struct {
	Warn *float64 `yaml:"warn"`
	Fail *float64 `yaml:"fail"`
}

// MetricMin is a lower bound on a metric of the model's metrics file.
type MetricMin struct {
	Min *float64 `yaml:"min"`
}

// Drift is packs.ml.drift.
type Drift struct {
	DriftPaths   `yaml:",inline"`
	Metric       string               `yaml:"metric"`
	Warn         *float64             `yaml:"warn"`
	Fail         *float64             `yaml:"fail"`
	MaxWarnShare *float64             `yaml:"max_warn_share"`
	Features     map[string]Threshold `yaml:"features"`
	Metrics      map[string]MetricMin `yaml:"metrics"`
	InDoD        bool                 `yaml:"in_dod"`
}

// Settings is the part of packs.ml this workstream reads.
type Settings struct {
	Paths struct {
		Train    []string `yaml:"train"`
		Features []string `yaml:"features"`
		Eval     []string `yaml:"eval"`
	} `yaml:"paths"`
	DataManifest string    `yaml:"data_manifest"`
	Models       []Model   `yaml:"models"`
	Promotion    Promotion `yaml:"promotion"`
	Drift        Drift     `yaml:"drift"`
}

// Drift defaults (spec C §4.1).
const (
	DefaultWarn         = 0.1
	DefaultFail         = 0.25
	DefaultMaxWarnShare = 0.3
	PromotionTimeout    = 300 * time.Second
	CurrentTimeout      = 600 * time.Second
	CurrentMaxAge       = 24 * time.Hour
)

// RequireItems are the promotion.require values.
var RequireItems = []string{"model_card", "drift", "repro"}

// load decodes the merged packs.ml settings. A decode error leaves the zero
// value: the validators already rejected bad manifests.
func load(s packs.Settings) Settings {
	var out Settings
	_ = s.Decode(&out)
	// The first entry of a name wins: the manifest's entries come before the
	// repo file's in the union.
	seen := map[string]bool{}
	var models []Model
	for _, m := range out.Models {
		if m.Name == "" || seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		models = append(models, m)
	}
	out.Models = models
	if len(out.Promotion.Require) == 0 {
		out.Promotion.Require = append([]string(nil), RequireItems...)
	}
	if out.Drift.Metric == "" {
		out.Drift.Metric = "psi"
	}
	return out
}

// Model returns the named model.
func (s Settings) Model(name string) (Model, bool) {
	for _, m := range s.Models {
		if m.Name == name {
			return m, true
		}
	}
	return Model{}, false
}

// CodePaths are the globs of a model's training, feature and eval code.
func (s Settings) CodePaths(m Model) []string {
	if len(m.Paths) > 0 {
		return m.Paths
	}
	var out []string
	out = append(out, s.Paths.Train...)
	out = append(out, s.Paths.Features...)
	return append(out, s.Paths.Eval...)
}

// Protected reports whether target needs an approval.
func (s Settings) Protected(target string) bool {
	for _, p := range s.Promotion.Protected {
		if strings.EqualFold(p, target) {
			return true
		}
	}
	return false
}

// Requires reports whether a promotion.require item is set.
func (s Settings) Requires(item string) bool {
	for _, r := range s.Promotion.Require {
		if r == item {
			return true
		}
	}
	return false
}

// DriftFor is the drift config with a model's own inputs applied.
func (s Settings) DriftFor(m Model) Drift {
	d := s.Drift
	if m.Drift != nil {
		if m.Drift.Reference != "" {
			d.Reference = m.Drift.Reference
		}
		if m.Drift.Current != "" {
			d.Current = m.Drift.Current
		}
		if m.Drift.CurrentCommand != "" {
			d.CurrentCommand = m.Drift.CurrentCommand
		}
	}
	return d
}

// ValidFor is promotion.valid_for (0 = the pack default).
func (s Settings) ValidFor() time.Duration {
	if s.Promotion.ValidFor == "" {
		return 0
	}
	d, _ := manifest.ParseDuration(s.Promotion.ValidFor)
	return d
}

func fl(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

// --- validation (registered as ConfigKeys) ---

var (
	modelNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	targetRe    = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,100}$`)
	placeRe     = regexp.MustCompile(`\{([^}]*)\}`)
	plainWordRe = regexp.MustCompile(`^[A-Za-z0-9._@/:=+,-]+$`)
)

func keysOf(n yaml.Node, what string, allowed ...string) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: %s must be a mapping", n.Line, what)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		ok := false
		for _, a := range allowed {
			if a == k {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("line %d: unknown key %q in %s (known: %s)", n.Content[i].Line, k, what, strings.Join(allowed, ", "))
		}
	}
	return nil
}

func child(n yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func repoPath(n *yaml.Node, what string) error {
	if n == nil {
		return nil
	}
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: %s must be a path", n.Line, what)
	}
	c := path.Clean(strings.ReplaceAll(n.Value, `\`, "/"))
	if n.Value == "" || path.IsAbs(c) || (len(c) > 1 && c[1] == ':') || c == ".." || strings.HasPrefix(c, "../") {
		return fmt.Errorf("line %d: %s %q must be a path inside the repository", n.Line, what, n.Value)
	}
	return nil
}

func number(n *yaml.Node, what string, lo, hi float64) error {
	if n == nil {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(n.Value), 64)
	if n.Kind != yaml.ScalarNode || err != nil || f < lo || f > hi {
		return fmt.Errorf("line %d: %s must be a number between %g and %g", n.Line, what, lo, hi)
	}
	return nil
}

func stringList(n *yaml.Node, what string, re *regexp.Regexp) error {
	if n == nil {
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: %s must be a list", n.Line, what)
	}
	for _, it := range n.Content {
		if it.Kind != yaml.ScalarNode || (re != nil && !re.MatchString(it.Value)) || it.Value == "" {
			return fmt.Errorf("line %d: %s: bad entry %q", it.Line, what, it.Value)
		}
	}
	return nil
}

func validateCommand(n *yaml.Node, what string, allowed ...string) error {
	if n == nil {
		return nil
	}
	if n.Kind != yaml.ScalarNode || strings.TrimSpace(n.Value) == "" {
		return fmt.Errorf("line %d: %s must be a command string", n.Line, what)
	}
	if _, err := SplitCommand(n.Value); err != nil {
		return fmt.Errorf("line %d: %s: %v", n.Line, what, err)
	}
	for _, m := range placeRe.FindAllStringSubmatch(n.Value, -1) {
		ok := false
		for _, a := range allowed {
			if a == m[1] {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("line %d: %s: unknown placeholder {%s} (known: %s)", n.Line, what, m[1], strings.Join(allowed, ", "))
		}
	}
	return nil
}

func validateModels(n yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: models must be a list", n.Line)
	}
	seen := map[string]bool{}
	for _, m := range n.Content {
		if err := keysOf(*m, "a model", "name", "card", "metrics", "paths", "drift"); err != nil {
			return err
		}
		name := child(*m, "name")
		if name == nil || !modelNameRe.MatchString(name.Value) {
			return fmt.Errorf("line %d: a model needs a name of [A-Za-z0-9._-]", m.Line)
		}
		if seen[name.Value] {
			return fmt.Errorf("line %d: model %q is listed twice", name.Line, name.Value)
		}
		seen[name.Value] = true
		if child(*m, "card") == nil {
			return fmt.Errorf("line %d: model %q needs a card path", m.Line, name.Value)
		}
		for _, k := range []string{"card", "metrics"} {
			if err := repoPath(child(*m, k), k); err != nil {
				return err
			}
		}
		if err := stringList(child(*m, "paths"), "paths", nil); err != nil {
			return err
		}
		if d := child(*m, "drift"); d != nil {
			if err := keysOf(*d, "a model's drift", "reference", "current", "current_command"); err != nil {
				return err
			}
			if err := driftPathKeys(*d); err != nil {
				return err
			}
		}
	}
	return nil
}

func driftPathKeys(n yaml.Node) error {
	for _, k := range []string{"reference", "current"} {
		if err := repoPath(child(n, k), "drift."+k); err != nil {
			return err
		}
	}
	return validateCommand(child(n, "current_command"), "drift.current_command", "model")
}

func validatePromotion(n yaml.Node) error {
	if err := keysOf(n, "promotion", "protected", "command", "valid_for", "require"); err != nil {
		return err
	}
	if err := stringList(child(n, "protected"), "promotion.protected", targetRe); err != nil {
		return err
	}
	if err := validateCommand(child(n, "command"), "promotion.command", "model", "version", "target"); err != nil {
		return err
	}
	if v := child(n, "valid_for"); v != nil {
		d, err := manifest.ParseDuration(v.Value)
		if err != nil || d <= 0 || d > 30*24*time.Hour {
			return fmt.Errorf("line %d: promotion.valid_for must be a duration up to 30d", v.Line)
		}
	}
	if r := child(n, "require"); r != nil {
		if err := stringList(r, "promotion.require", nil); err != nil {
			return err
		}
		for _, it := range r.Content {
			ok := false
			for _, a := range RequireItems {
				if a == it.Value {
					ok = true
				}
			}
			if !ok {
				return fmt.Errorf("line %d: promotion.require: unknown item %q (known: %s)", it.Line, it.Value, strings.Join(RequireItems, ", "))
			}
		}
	}
	return nil
}

func validateDrift(n yaml.Node) error {
	if err := keysOf(n, "drift", "reference", "current", "current_command", "metric", "warn", "fail", "max_warn_share", "features", "metrics", "in_dod"); err != nil {
		return err
	}
	if err := driftPathKeys(n); err != nil {
		return err
	}
	if m := child(n, "metric"); m != nil && m.Value != "psi" && m.Value != "js" {
		return fmt.Errorf("line %d: drift.metric must be psi or js", m.Line)
	}
	for _, k := range []string{"warn", "fail"} {
		if err := number(child(n, k), "drift."+k, 0, 100); err != nil {
			return err
		}
	}
	if err := number(child(n, "max_warn_share"), "drift.max_warn_share", 0, 1); err != nil {
		return err
	}
	w, f := child(n, "warn"), child(n, "fail")
	if w != nil && f != nil {
		a, _ := strconv.ParseFloat(w.Value, 64)
		b, _ := strconv.ParseFloat(f.Value, 64)
		if a > b {
			return fmt.Errorf("line %d: drift.warn must not be above drift.fail", w.Line)
		}
	}
	if fs := child(n, "features"); fs != nil {
		if fs.Kind != yaml.MappingNode {
			return fmt.Errorf("line %d: drift.features must be a mapping of feature names", fs.Line)
		}
		for i := 0; i+1 < len(fs.Content); i += 2 {
			v := *fs.Content[i+1]
			if err := keysOf(v, "drift.features."+fs.Content[i].Value, "warn", "fail"); err != nil {
				return err
			}
			for _, k := range []string{"warn", "fail"} {
				if err := number(child(v, k), "drift.features."+fs.Content[i].Value+"."+k, 0, 100); err != nil {
					return err
				}
			}
		}
	}
	if ms := child(n, "metrics"); ms != nil {
		if ms.Kind != yaml.MappingNode {
			return fmt.Errorf("line %d: drift.metrics must be a mapping of metric names", ms.Line)
		}
		for i := 0; i+1 < len(ms.Content); i += 2 {
			v := *ms.Content[i+1]
			if err := keysOf(v, "drift.metrics."+ms.Content[i].Value, "min"); err != nil {
				return err
			}
			mn := child(v, "min")
			if mn == nil {
				return fmt.Errorf("line %d: drift.metrics.%s needs min", v.Line, ms.Content[i].Value)
			}
			if err := number(mn, "drift.metrics."+ms.Content[i].Value+".min", -1e12, 1e12); err != nil {
				return err
			}
		}
	}
	if b := child(n, "in_dod"); b != nil && b.Value != "true" && b.Value != "false" {
		return fmt.Errorf("line %d: drift.in_dod must be true or false", b.Line)
	}
	return nil
}

// --- commands from config ---

// ErrCommand marks a command string tq refuses to split.
var ErrCommand = errors.New("bad command")

// SplitCommand splits a command string into argv without a shell: words are
// separated by spaces, '...' and "..." quote, and shell operators (| & ; < >
// $ `) are refused so a config command can never become a pipeline.
func SplitCommand(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	in, quote := false, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, in = c, true
		case c == ' ' || c == '\t':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		case strings.IndexByte("|&;<>$`\n\r", c) >= 0:
			return nil, fmt.Errorf("%w: shell operator %q is not allowed (tq runs the command without a shell)", ErrCommand, string(c))
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("%w: unterminated quote", ErrCommand)
	}
	if in {
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: empty command", ErrCommand)
	}
	return out, nil
}

// FillCommand splits tmpl and fills {name} placeholders inside each word.
// Values must match [A-Za-z0-9._@-]: they cannot add a word or an option.
func FillCommand(tmpl string, vals map[string]string) ([]string, error) {
	for k, v := range vals {
		if !targetRe.MatchString(v) || strings.HasPrefix(v, "-") {
			return nil, fmt.Errorf("%s %q: only [A-Za-z0-9._@-] is allowed, not starting with -", k, v)
		}
	}
	argv, err := SplitCommand(tmpl)
	if err != nil {
		return nil, err
	}
	for i, a := range argv {
		var bad error
		argv[i] = placeRe.ReplaceAllStringFunc(a, func(m string) string {
			v, ok := vals[m[1:len(m)-1]]
			if !ok {
				bad = fmt.Errorf("%w: unknown placeholder %s", ErrCommand, m)
			}
			return v
		})
		if bad != nil {
			return nil, bad
		}
	}
	return argv, nil
}

// QuoteArgv prints argv as one shell-quoted line for the human to read.
func QuoteArgv(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && plainWordRe.MatchString(a) {
			q[i] = a
		} else {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(q, " ")
}

// scriptWords are the words of a command that name a file or a module
// (scripts/promote.py, -m pkg.promote): the guard and promotion-in-code use
// them to recognise the declared command.
func scriptWords(cmd string) []string {
	argv, err := SplitCommand(cmd)
	if err != nil {
		return nil
	}
	var out []string
	for i, a := range argv {
		if placeRe.MatchString(a) {
			continue
		}
		switch {
		case a == "-m" && i+1 < len(argv):
			out = append(out, strings.ReplaceAll(argv[i+1], ".", "/")+".py")
		case strings.Contains(a, "/") || strings.HasSuffix(a, ".py") || strings.HasSuffix(a, ".sh") || strings.HasSuffix(a, ".ps1"):
			out = append(out, strings.TrimPrefix(strings.ReplaceAll(a, `\`, "/"), "./"))
		}
	}
	sort.Strings(out)
	return out
}
