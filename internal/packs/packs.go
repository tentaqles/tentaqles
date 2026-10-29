// Package packs is the pack framework (spec C §2): a registry of checks,
// guards, triage flags, reviewers, config keys, doctor probes and
// subcommands, grouped by pack (data, ml, cloud, db). Packs register from
// init in their own register_*.go files; the framework runs the checks
// (tq pack check), the guards (tq claude-hook), the flags (tq decide triage)
// and mounts the subcommands under `tq pack <pack>`.
package packs

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"gopkg.in/yaml.v3"
)

// Status is a check result: ok, fail or skip.
type Status string

const (
	OK   Status = "ok"
	Fail Status = "fail"
	Skip Status = "skip"
)

// Finding levels. Only an error finding fails a check.
const (
	LevelError = "error"
	LevelWarn  = "warn"
)

// Finding is one problem at file:line. It never carries a value from the
// scanned file (PII at the exits): names, rule ids and messages only.
type Finding struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Level   string `json:"level"` // error | warn
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// String is the text form `file:line level rule message`.
func (f Finding) String() string {
	loc := f.File
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return fmt.Sprintf("%s %s %s %s", loc, f.Level, f.Rule, f.Message)
}

// ToolRun records the external tool behind a result.
type ToolRun struct {
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	Path       string `json:"path,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out,omitempty"`
}

// Result is the outcome of one check.
type Result struct {
	ID       string    `json:"id"`
	Pack     string    `json:"pack"`
	Status   Status    `json:"status"`
	Required bool      `json:"required"`
	Detail   string    `json:"detail"`
	Findings []Finding `json:"findings"`
	Tool     *ToolRun  `json:"tool,omitempty"`
}

// Counts returns the finding counts by level.
func (r Result) Counts() (errors, warns int) {
	for _, f := range r.Findings {
		if f.Level == LevelError {
			errors++
		} else {
			warns++
		}
	}
	return
}

// Kind tells built-in (pure Go) checks from external (tool) checks.
type Kind string

const (
	Builtin  Kind = "builtin"
	External Kind = "external"
)

// Check is one rule with the id <pack>/<name>.
type Check struct {
	ID, Pack string
	Kind     Kind
	// Tool is the tool name used in packs.<p>.tools (External only).
	Tool string
	// MinVersion is the lowest tool version accepted (External only, optional).
	// A declared tool below it fails.
	MinVersion string
	// Timeout is the check's default tool timeout (External only; 120s when
	// zero). packs.<p>.timeouts.<tool> overrides it, capped at 1800s.
	Timeout time.Duration
	// Applies is cheap: paths and config only. Nil means always.
	Applies func(in *Input) bool
	// Run does the work. For an External check the framework has already
	// found the tool (in.Tools.Probe) before calling it.
	Run func(ctx context.Context, in *Input) Result
}

// Flag is a triage flag a pack raises, or (with no Raise and no Question,
// named after a core flag) a reviewer mapping onto a core flag.
type Flag struct {
	Name      string
	Reviewers []string
	// Raise is deterministic: paths and config only.
	Raise func(in *Input) bool
	// Question, when set, is asked of Jev in triage while the pack is enabled.
	Question string
}

// ReviewerOnlyFlags are the core flags a Flag with no Raise and no Question
// may name: they add reviewers when core triage raises them. baseline_change
// is the framework's own flag (raised when a pack baseline grows).
var ReviewerOnlyFlags = []string{"schema", "migration", "rls", "data_loss", "destructive", FlagBaselineChange}

// FlagBaselineChange is raised by the framework when a diff grows a pack's
// baseline file (spec C §2.6a).
const FlagBaselineChange = "baseline_change"

// Hint is a detection hint for packs-suggest: a glob (matched against the
// base name when it has no slash) and an optional content regex.
type Hint struct {
	Glob     string
	Contains string
	Why      string
	re       *regexp.Regexp
}

// MergeKind is how a pack-specific key of tq-packs.yaml merges with the
// manifest (spec C §2.3). Keys with no registered kind are trusted: the
// repo file may add them when the manifest lacks them, never change them.
type MergeKind int

const (
	MergeTrusted    MergeKind = iota
	MergeUnion                // lists: union (path lists)
	MergeMin                  // upper bounds: the smaller wins
	MergeMax                  // lower bounds: the larger wins
	MergeRepo                 // commands: the repo value is allowed
	MergeOr                   // booleans that only tighten when true
	MergeIgnoreRepo           // trusted-layer only (like tool_paths)
)

// Pack is one pack's registration. Several register_*.go files may each
// register parts of one pack: RegisterPack merges by Name.
type Pack struct {
	Name string
	// ConfigKeys validate the pack's own keys of packs.<name>; an unknown key
	// is an error once any key is registered for the pack.
	ConfigKeys map[string]func(yaml.Node) error
	// Merge sets how pack keys merge with tq-packs.yaml, by dotted path
	// ("contracts.paths", "drift.metrics.*.min"; * is one key).
	Merge    map[string]MergeKind
	Flags    []Flag
	Hints    []Hint
	Doctor   func(in *Input) []doctor.Finding
	Commands func() []*cobra.Command
}

type packEntry struct {
	name       string
	keys       map[string]func(yaml.Node) error
	merge      map[string]MergeKind
	flags      []Flag
	hints      []Hint
	doctors    []func(*Input) []doctor.Finding
	commands   []func() []*cobra.Command
	cmdNames   map[string]bool
	validating bool
}

var (
	regMu  sync.RWMutex
	checks = map[string]Check{}
	pks    = map[string]*packEntry{}
	guards = map[string][]Guard{} // id -> guards (one id may serve several packs)
)

func knownPack(name string) bool {
	for _, p := range manifest.KnownPacks {
		if p == name {
			return true
		}
	}
	return false
}

// Register adds a check. It panics on a duplicate id, an id outside its
// pack, an unknown pack or a missing Run.
func Register(c Check) {
	regMu.Lock()
	defer regMu.Unlock()
	if !knownPack(c.Pack) {
		panic(fmt.Sprintf("packs.Register %s: unknown pack %q", c.ID, c.Pack))
	}
	if !strings.HasPrefix(c.ID, c.Pack+"/") || len(c.ID) == len(c.Pack)+1 {
		panic(fmt.Sprintf("packs.Register: id %q must be %s/<name>", c.ID, c.Pack))
	}
	if c.Run == nil {
		panic("packs.Register " + c.ID + ": Run is nil")
	}
	switch c.Kind {
	case Builtin:
	case External:
		if c.Tool == "" {
			panic("packs.Register " + c.ID + ": an external check needs Tool")
		}
	default:
		panic("packs.Register " + c.ID + ": Kind must be Builtin or External")
	}
	if _, dup := checks[c.ID]; dup {
		panic("packs.Register: duplicate check id " + c.ID)
	}
	checks[c.ID] = c
	entry(c.Pack)
}

// entry returns the pack's entry, creating it. regMu must be held.
func entry(name string) *packEntry {
	e := pks[name]
	if e == nil {
		e = &packEntry{name: name, keys: map[string]func(yaml.Node) error{}, merge: map[string]MergeKind{}, cmdNames: map[string]bool{}}
		pks[name] = e
	}
	return e
}

// RegisterPack merges a pack's config keys, merge kinds, flags, hints,
// doctor probe and commands. It panics on an unknown pack, a duplicate
// config key, merge path, flag or command name, a framework key, a bad hint
// regex, or a reviewer-only flag that names no core flag.
func RegisterPack(p Pack) {
	regMu.Lock()
	defer regMu.Unlock()
	if !knownPack(p.Name) {
		panic(fmt.Sprintf("packs.RegisterPack: unknown pack %q", p.Name))
	}
	e := entry(p.Name)
	for k, fn := range p.ConfigKeys {
		for _, fk := range manifest.FrameworkPackKeys {
			if k == fk {
				panic("packs.RegisterPack " + p.Name + ": " + k + " is a framework key")
			}
		}
		if _, dup := e.keys[k]; dup {
			panic("packs.RegisterPack " + p.Name + ": duplicate config key " + k)
		}
		e.keys[k] = fn
	}
	for k, m := range p.Merge {
		if _, dup := e.merge[k]; dup {
			panic("packs.RegisterPack " + p.Name + ": duplicate merge path " + k)
		}
		e.merge[k] = m
	}
	for _, f := range p.Flags {
		if f.Name == "" {
			panic("packs.RegisterPack " + p.Name + ": a flag needs a name")
		}
		for _, have := range e.flags {
			if have.Name == f.Name {
				panic("packs.RegisterPack " + p.Name + ": duplicate flag " + f.Name)
			}
		}
		if f.Raise == nil && f.Question == "" && !contains(ReviewerOnlyFlags, f.Name) {
			panic(fmt.Sprintf("packs.RegisterPack %s: reviewer-only flag %q must name a core flag (%s)", p.Name, f.Name, strings.Join(ReviewerOnlyFlags, ", ")))
		}
		e.flags = append(e.flags, f)
	}
	for _, h := range p.Hints {
		if h.Glob == "" {
			panic("packs.RegisterPack " + p.Name + ": a hint needs a glob")
		}
		if h.Contains != "" {
			re, err := regexp.Compile(h.Contains)
			if err != nil {
				panic("packs.RegisterPack " + p.Name + ": hint regex: " + err.Error())
			}
			h.re = re
		}
		e.hints = append(e.hints, h)
	}
	if p.Doctor != nil {
		e.doctors = append(e.doctors, p.Doctor)
	}
	if p.Commands != nil {
		for _, c := range p.Commands() {
			n := c.Name()
			if e.cmdNames[n] {
				panic("packs.RegisterPack " + p.Name + ": duplicate command " + n)
			}
			e.cmdNames[n] = true
		}
		e.commands = append(e.commands, p.Commands)
	}
	if len(e.keys) > 0 && !e.validating {
		e.validating = true
		name := p.Name
		manifest.RegisterPackValidator(name, func(n yaml.Node) error { return validateKeys(name, n) })
	}
}

// validateKeys checks every non-framework key of a packs.<name> node against
// the registered config keys.
func validateKeys(name string, n yaml.Node) error {
	regMu.RLock()
	e := pks[name]
	keys := map[string]func(yaml.Node) error{}
	if e != nil {
		for k, v := range e.keys {
			keys[k] = v
		}
	}
	regMu.RUnlock()
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if contains(manifest.FrameworkPackKeys, k) {
			continue
		}
		fn, ok := keys[k]
		if !ok {
			known := make([]string, 0, len(keys))
			for kk := range keys {
				known = append(known, kk)
			}
			sort.Strings(known)
			return fmt.Errorf("line %d: unknown key %q (known: %s)", n.Content[i].Line, k, strings.Join(append(append([]string{}, manifest.FrameworkPackKeys...), known...), ", "))
		}
		if fn != nil {
			if err := fn(*n.Content[i+1]); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
	}
	return nil
}

// Checks returns the checks of pack (all packs when ""), sorted by id.
func Checks(pack string) []Check {
	regMu.RLock()
	defer regMu.RUnlock()
	var out []Check
	for _, c := range checks {
		if pack == "" || c.Pack == pack {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Names returns every known pack name, sorted.
func Names() []string {
	out := append([]string(nil), manifest.KnownPacks...)
	sort.Strings(out)
	return out
}

// Flags returns pack's registered flags.
func Flags(pack string) []Flag {
	regMu.RLock()
	defer regMu.RUnlock()
	if e := pks[pack]; e != nil {
		return append([]Flag(nil), e.flags...)
	}
	return nil
}

// Hints returns pack's detection hints.
func Hints(pack string) []Hint {
	regMu.RLock()
	defer regMu.RUnlock()
	if e := pks[pack]; e != nil {
		return append([]Hint(nil), e.hints...)
	}
	return nil
}

// Commands returns fresh subcommands registered for pack (each call builds
// new cobra commands, so several roots never share one).
func Commands(pack string) []*cobra.Command {
	regMu.RLock()
	var fs []func() []*cobra.Command
	if e := pks[pack]; e != nil {
		fs = append(fs, e.commands...)
	}
	regMu.RUnlock()
	var out []*cobra.Command
	for _, f := range fs {
		out = append(out, f()...)
	}
	return out
}

func doctors(pack string) []func(*Input) []doctor.Finding {
	regMu.RLock()
	defer regMu.RUnlock()
	if e := pks[pack]; e != nil {
		return append([]func(*Input) []doctor.Finding(nil), e.doctors...)
	}
	return nil
}

func mergeKinds(pack string) map[string]MergeKind {
	regMu.RLock()
	defer regMu.RUnlock()
	out := map[string]MergeKind{}
	if e := pks[pack]; e != nil {
		for k, v := range e.merge {
			out[k] = v
		}
	}
	return out
}

// Questions returns the Jev questions of the enabled packs' flags, keyed by
// flag name. A disabled pack adds none.
func Questions(cfg Config) map[string]decide.Question {
	out := map[string]decide.Question{}
	for _, p := range cfg.Enabled() {
		for _, f := range Flags(p) {
			if f.Question != "" {
				out[f.Name] = decide.Noul(f.Question)
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
